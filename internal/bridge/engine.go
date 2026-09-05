package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxScannerBytes = 64 << 20

type EngineConfig struct {
	Executable      string
	Model           string
	Config          string
	WorkingDir      string
	Environment     []string
	MaxVisits       int64
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
}

type Sender interface {
	Send(context.Context, []byte) error
	SessionID() string
}

type route struct {
	sender            Sender
	originalID        string
	remainingFinals   int
	action            string
	terminateOriginal string
}

type Engine struct {
	cfg    EngineConfig
	logger *log.Logger

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	done   chan error

	writeMu sync.Mutex
	routeMu sync.Mutex
	routes  map[string]*route
	byUser  map[string]map[string]string
	probes  map[string]chan map[string]any
	seq     atomic.Uint64
}

func Start(ctx context.Context, cfg EngineConfig, logger *log.Logger) (*Engine, error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	if cfg.Executable == "" || cfg.Model == "" || cfg.Config == "" {
		return nil, errors.New("KataGo executable, model, and analysis config are required")
	}
	if cfg.MaxVisits <= 0 {
		cfg.MaxVisits = 1_000_000
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = 45 * time.Second
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 8 * time.Second
	}

	engineCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(
		engineCtx,
		cfg.Executable,
		"analysis",
		"-model",
		cfg.Model,
		"-config",
		cfg.Config,
	)
	cmd.Dir = cfg.WorkingDir
	cmd.Env = append(os.Environ(), cfg.Environment...)
	configureProcessGroup(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open KataGo stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open KataGo stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open KataGo stderr: %w", err)
	}

	engine := &Engine{
		cfg:    cfg,
		logger: logger,
		cmd:    cmd,
		stdin:  stdin,
		cancel: cancel,
		done:   make(chan error, 1),
		routes: make(map[string]*route),
		byUser: make(map[string]map[string]string),
		probes: make(map[string]chan map[string]any),
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start KataGo: %w", err)
	}
	logger.Printf("KataGo process started (pid=%d)", cmd.Process.Pid)
	go engine.readStdout(stdout)
	go engine.readStderr(stderr)
	go func() {
		err := cmd.Wait()
		engine.done <- err
		close(engine.done)
	}()

	probeCtx, probeCancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer probeCancel()
	if _, err := engine.probe(probeCtx); err != nil {
		_ = engine.Shutdown(context.Background())
		return nil, fmt.Errorf("KataGo startup check failed: %w", err)
	}
	logger.Printf("KataGo analysis protocol is ready")
	return engine, nil
}

func (e *Engine) probe(ctx context.Context) (map[string]any, error) {
	id := fmt.Sprintf("__remote_probe_%d", e.seq.Add(1))
	result := make(chan map[string]any, 1)
	e.routeMu.Lock()
	e.probes[id] = result
	e.routeMu.Unlock()
	defer func() {
		e.routeMu.Lock()
		delete(e.probes, id)
		e.routeMu.Unlock()
	}()
	if err := e.writeJSON(map[string]any{"id": id, "action": "query_version"}); err != nil {
		return nil, err
	}
	select {
	case response := <-result:
		if message, ok := response["error"].(string); ok && message != "" {
			return nil, errors.New(message)
		}
		return response, nil
	case err := <-e.done:
		if err == nil {
			err = errors.New("KataGo exited before replying")
		}
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *Engine) Submit(ctx context.Context, sender Sender, payload []byte) error {
	query, originalID, err := e.validateQuery(payload)
	if err != nil {
		return e.sendProtocolError(ctx, sender, originalID, err)
	}

	sessionID := sender.SessionID()
	e.routeMu.Lock()
	userRoutes := e.byUser[sessionID]
	if userRoutes == nil {
		userRoutes = make(map[string]string)
		e.byUser[sessionID] = userRoutes
	}
	if _, exists := userRoutes[originalID]; exists {
		e.routeMu.Unlock()
		return e.sendProtocolError(ctx, sender, originalID, errors.New("query id is already active"))
	}

	action, _ := query["action"].(string)
	actualID := fmt.Sprintf("remote_%s_%d", sessionID, e.seq.Add(1))
	r := &route{
		sender:          sender,
		originalID:      originalID,
		remainingFinals: expectedFinals(query),
		action:          action,
	}
	if action == "terminate" {
		target, ok := query["terminateId"].(string)
		if !ok || target == "" {
			e.routeMu.Unlock()
			return e.sendProtocolError(ctx, sender, originalID, errors.New("terminateId is required"))
		}
		mapped, ok := userRoutes[target]
		if !ok {
			e.routeMu.Unlock()
			return e.sendProtocolError(ctx, sender, originalID, errors.New("terminateId does not name an active query"))
		}
		query["terminateId"] = mapped
		r.terminateOriginal = target
	}
	query["id"] = actualID
	e.routes[actualID] = r
	userRoutes[originalID] = actualID
	e.routeMu.Unlock()

	if err := e.writeJSON(query); err != nil {
		e.removeRoute(actualID, r)
		return fmt.Errorf("send query to KataGo: %w", err)
	}
	return nil
}

func (e *Engine) validateQuery(payload []byte) (map[string]any, string, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var query map[string]any
	if err := decoder.Decode(&query); err != nil {
		return nil, "invalid-request", errors.New("request must be one JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, "invalid-request", errors.New("request must contain exactly one JSON object")
	}
	if query == nil {
		return nil, "invalid-request", errors.New("request must be one JSON object")
	}
	originalID, ok := query["id"].(string)
	if !ok || strings.TrimSpace(originalID) == "" || len(originalID) > 256 {
		return nil, "invalid-request", errors.New("id must be a non-empty string of at most 256 characters")
	}
	if action, exists := query["action"]; exists {
		value, ok := action.(string)
		if !ok || !allowedAction(value) {
			return nil, originalID, errors.New("unsupported analysis action")
		}
	}
	if value, exists := query["maxVisits"]; exists {
		visits, err := integerValue(value)
		if err != nil || visits < 1 || visits > e.cfg.MaxVisits {
			return nil, originalID, fmt.Errorf("maxVisits must be between 1 and %d", e.cfg.MaxVisits)
		}
	}
	if turns, ok := query["analyzeTurns"].([]any); ok && len(turns) > 4096 {
		return nil, originalID, errors.New("analyzeTurns exceeds the 4096-position safety limit")
	}
	for _, key := range []string{"moves", "initialStones"} {
		if moves, ok := query[key].([]any); ok && len(moves) > 10_000 {
			return nil, originalID, fmt.Errorf("%s exceeds the safety limit", key)
		}
	}
	return query, originalID, nil
}

func allowedAction(action string) bool {
	switch action {
	case "terminate", "terminate_all", "query_version", "query_models", "clear_cache":
		return true
	default:
		return false
	}
}

func integerValue(value any) (int64, error) {
	switch number := value.(type) {
	case json.Number:
		return strconv.ParseInt(number.String(), 10, 64)
	case float64:
		if number != float64(int64(number)) {
			return 0, errors.New("not an integer")
		}
		return int64(number), nil
	default:
		return 0, errors.New("not a number")
	}
}

func expectedFinals(query map[string]any) int {
	if _, special := query["action"]; special {
		return 1
	}
	if turns, ok := query["analyzeTurns"].([]any); ok && len(turns) > 0 {
		return len(turns)
	}
	return 1
}

func (e *Engine) sendProtocolError(ctx context.Context, sender Sender, id string, err error) error {
	data, _ := json.Marshal(map[string]any{"id": id, "error": err.Error()})
	if sendErr := sender.Send(ctx, data); sendErr != nil {
		return sendErr
	}
	return nil
}

func (e *Engine) writeJSON(value map[string]any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	if e.stdin == nil {
		return errors.New("KataGo stdin is closed")
	}
	_, err = e.stdin.Write(append(data, '\n'))
	return err
}

func (e *Engine) readStdout(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), maxScannerBytes)
	for scanner.Scan() {
		e.dispatch(scanner.Bytes())
	}
	if err := scanner.Err(); err != nil {
		e.logger.Printf("KataGo stdout failed: %v", err)
	}
}

func (e *Engine) readStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		e.logger.Printf("katago: %s", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		e.logger.Printf("KataGo stderr failed: %v", err)
	}
}

func (e *Engine) dispatch(line []byte) {
	decoder := json.NewDecoder(strings.NewReader(string(line)))
	decoder.UseNumber()
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		e.logger.Printf("ignored non-JSON KataGo output: %v", err)
		return
	}
	actualID, ok := response["id"].(string)
	if !ok || actualID == "" {
		e.logger.Printf("ignored KataGo response without an id")
		return
	}

	e.routeMu.Lock()
	if probe := e.probes[actualID]; probe != nil {
		e.routeMu.Unlock()
		select {
		case probe <- response:
		default:
		}
		return
	}
	r := e.routes[actualID]
	if r == nil {
		e.routeMu.Unlock()
		return
	}
	response["id"] = r.originalID
	if _, exists := response["terminateId"]; exists && r.terminateOriginal != "" {
		response["terminateId"] = r.terminateOriginal
	}
	data, err := json.Marshal(response)
	remove := shouldComplete(response, r)
	if remove {
		delete(e.routes, actualID)
		if users := e.byUser[r.sender.SessionID()]; users != nil {
			delete(users, r.originalID)
			if len(users) == 0 {
				delete(e.byUser, r.sender.SessionID())
			}
		}
	}
	e.routeMu.Unlock()
	if err != nil {
		e.logger.Printf("encode KataGo response: %v", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.sender.Send(ctx, data); err != nil {
		e.logger.Printf("deliver KataGo response: %v", err)
	}
}

func shouldComplete(response map[string]any, r *route) bool {
	if _, failed := response["error"]; failed {
		return true
	}
	if r.action != "" {
		return true
	}
	if during, _ := response["isDuringSearch"].(bool); during {
		return false
	}
	r.remainingFinals--
	return r.remainingFinals <= 0
}

func (e *Engine) removeRoute(actualID string, expected *route) {
	e.routeMu.Lock()
	defer e.routeMu.Unlock()
	if e.routes[actualID] != expected {
		return
	}
	delete(e.routes, actualID)
	if users := e.byUser[expected.sender.SessionID()]; users != nil {
		delete(users, expected.originalID)
		if len(users) == 0 {
			delete(e.byUser, expected.sender.SessionID())
		}
	}
}

func (e *Engine) CancelSession(sessionID string) {
	e.routeMu.Lock()
	users := e.byUser[sessionID]
	actualIDs := make([]string, 0, len(users))
	for _, actualID := range users {
		if r := e.routes[actualID]; r != nil && r.action == "" {
			actualIDs = append(actualIDs, actualID)
		}
		delete(e.routes, actualID)
	}
	delete(e.byUser, sessionID)
	e.routeMu.Unlock()

	for _, actualID := range actualIDs {
		_ = e.writeJSON(map[string]any{
			"id":          fmt.Sprintf("__remote_cancel_%d", e.seq.Add(1)),
			"action":      "terminate",
			"terminateId": actualID,
		})
	}
}

func (e *Engine) Done() <-chan error {
	return e.done
}

func (e *Engine) Shutdown(ctx context.Context) error {
	if e.stdin != nil {
		_ = e.writeJSON(map[string]any{
			"id":     fmt.Sprintf("__remote_shutdown_%d", e.seq.Add(1)),
			"action": "terminate_all",
		})
		e.writeMu.Lock()
		_ = e.stdin.Close()
		e.stdin = nil
		e.writeMu.Unlock()
	}
	select {
	case err := <-e.done:
		e.cancel()
		return err
	case <-time.After(e.cfg.ShutdownTimeout):
		killProcessGroup(e.cmd)
		e.cancel()
		select {
		case <-e.done:
		case <-ctx.Done():
		}
		return nil
	case <-ctx.Done():
		killProcessGroup(e.cmd)
		e.cancel()
		return ctx.Err()
	}
}
