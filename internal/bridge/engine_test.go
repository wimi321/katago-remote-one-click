package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type channelSender struct {
	id       string
	messages chan []byte
}

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_KATAGO_HELPER") == "1" {
		runFakeKataGoProcess()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func (s *channelSender) SessionID() string { return s.id }

func (s *channelSender) Send(_ context.Context, payload []byte) error {
	copyOfPayload := append([]byte(nil), payload...)
	s.messages <- copyOfPayload
	return nil
}

func TestEnginePreservesIDsAndWaitsForEveryAnalyzedTurn(t *testing.T) {
	engine := startFakeEngine(t)
	sender := &channelSender{id: "one", messages: make(chan []byte, 8)}
	query := []byte(`{"id":"review","boardXSize":19,"boardYSize":19,"analyzeTurns":[0,1]}`)
	if err := engine.Submit(context.Background(), sender, query); err != nil {
		t.Fatal(err)
	}

	for turn := 0; turn < 2; turn++ {
		response := receiveJSON(t, sender.messages)
		if response["id"] != "review" {
			t.Fatalf("id = %v, want review", response["id"])
		}
		if got := int(response["turnNumber"].(float64)); got != turn {
			t.Fatalf("turnNumber = %d, want %d", got, turn)
		}
	}

	engine.routeMu.Lock()
	defer engine.routeMu.Unlock()
	if len(engine.routes) != 0 {
		t.Fatalf("routes still active after all final responses: %d", len(engine.routes))
	}
}

func TestEngineMapsTerminateIDWithoutLeakingInternalIDs(t *testing.T) {
	engine := startFakeEngine(t)
	sender := &channelSender{id: "stopper", messages: make(chan []byte, 8)}
	if err := engine.Submit(context.Background(), sender, []byte(`{"id":"slow","testDelayMs":500}`)); err != nil {
		t.Fatal(err)
	}
	if err := engine.Submit(
		context.Background(),
		sender,
		[]byte(`{"id":"stop","action":"terminate","terminateId":"slow"}`),
	); err != nil {
		t.Fatal(err)
	}
	response := receiveJSON(t, sender.messages)
	if response["id"] != "stop" || response["terminateId"] != "slow" {
		t.Fatalf("unexpected terminate response: %#v", response)
	}
	encoded, _ := json.Marshal(response)
	if strings.Contains(string(encoded), "remote_") {
		t.Fatalf("internal id leaked: %s", encoded)
	}
}

func TestCancelSessionDropsLateResults(t *testing.T) {
	engine := startFakeEngine(t)
	oldSender := &channelSender{id: "old", messages: make(chan []byte, 8)}
	newSender := &channelSender{id: "new", messages: make(chan []byte, 8)}
	if err := engine.Submit(context.Background(), oldSender, []byte(`{"id":"same","testDelayMs":250}`)); err != nil {
		t.Fatal(err)
	}
	engine.CancelSession(oldSender.id)
	if err := engine.Submit(context.Background(), newSender, []byte(`{"id":"same"}`)); err != nil {
		t.Fatal(err)
	}
	response := receiveJSON(t, newSender.messages)
	if response["id"] != "same" {
		t.Fatalf("new response id = %v", response["id"])
	}
	select {
	case unexpected := <-oldSender.messages:
		t.Fatalf("old session received late result: %s", unexpected)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestEngineRejectsUnsafeRequestsAsProtocolErrors(t *testing.T) {
	engine := startFakeEngine(t)
	sender := &channelSender{id: "invalid", messages: make(chan []byte, 8)}
	for _, payload := range [][]byte{
		[]byte(`{"id":"too-deep","maxVisits":1000001}`),
		[]byte(`{"id":"bad-action","action":"shell"}`),
		[]byte(`{"id":"first"} {"id":"second"}`),
		[]byte(`not-json`),
	} {
		if err := engine.Submit(context.Background(), sender, payload); err != nil {
			t.Fatal(err)
		}
		response := receiveJSON(t, sender.messages)
		if _, ok := response["error"]; !ok {
			t.Fatalf("missing protocol error: %#v", response)
		}
	}
}

func startFakeEngine(t *testing.T) *Engine {
	t.Helper()
	directory := t.TempDir()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := Start(context.Background(), EngineConfig{
		Executable:      testBinary,
		Model:           filepath.Join(directory, "model.bin.gz"),
		Config:          filepath.Join(directory, "analysis.cfg"),
		WorkingDir:      directory,
		Environment:     []string{"GO_WANT_KATAGO_HELPER=1"},
		MaxVisits:       1_000_000,
		StartupTimeout:  5 * time.Second,
		ShutdownTimeout: time.Second,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = engine.Shutdown(ctx)
	})
	return engine
}

func receiveJSON(t *testing.T, messages <-chan []byte) map[string]any {
	t.Helper()
	select {
	case payload := <-messages:
		var response map[string]any
		if err := json.Unmarshal(payload, &response); err != nil {
			t.Fatalf("decode %q: %v", payload, err)
		}
		return response
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for response")
		return nil
	}
}

func runFakeKataGoProcess() {
	var writeMu sync.Mutex
	var workers sync.WaitGroup
	write := func(value map[string]any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		data, _ := json.Marshal(value)
		_, _ = fmt.Fprintln(os.Stdout, string(data))
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var query map[string]any
		if json.Unmarshal(scanner.Bytes(), &query) != nil {
			continue
		}
		workers.Add(1)
		go func(query map[string]any) {
			defer workers.Done()
			id, _ := query["id"].(string)
			action, _ := query["action"].(string)
			switch action {
			case "query_version":
				write(map[string]any{"id": id, "action": action, "version": "test"})
			case "terminate":
				write(map[string]any{"id": id, "action": action, "terminateId": query["terminateId"]})
			case "terminate_all", "clear_cache", "query_models":
				write(map[string]any{"id": id, "action": action})
			default:
				if value, ok := query["testDelayMs"].(float64); ok {
					time.Sleep(time.Duration(value) * time.Millisecond)
				}
				turns, _ := query["analyzeTurns"].([]any)
				if len(turns) == 0 {
					turns = []any{float64(0)}
				}
				for _, turn := range turns {
					write(map[string]any{
						"id":             id,
						"turnNumber":     turn,
						"isDuringSearch": false,
						"rootInfo":       map[string]any{"visits": 10},
						"moveInfos":      []any{},
					})
				}
			}
		}(query)
	}
	workers.Wait()
	if code := os.Getenv("GO_WANT_KATAGO_HELPER_EXIT"); code != "" {
		value, _ := strconv.Atoi(code)
		os.Exit(value)
	}
}
