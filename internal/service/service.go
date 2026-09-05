package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/wimi321/katago-remote-one-click/internal/bridge"
	appconfig "github.com/wimi321/katago-remote-one-click/internal/config"
)

type InitOptions struct {
	KataGo      string
	Model       string
	Config      string
	Cloudflared string
	Listen      string
	MaxVisits   int64
}

type Status struct {
	Running    bool
	PID        int
	Connection *ConnectionState
}

type Check struct {
	Name    string
	OK      bool
	Message string
}

func Initialize(home string, options InitOptions) error {
	paths := map[string]string{
		"KataGo":          options.KataGo,
		"model":           options.Model,
		"analysis config": options.Config,
		"cloudflared":     options.Cloudflared,
	}
	for name, path := range paths {
		if path == "" {
			return fmt.Errorf("%s path is required", name)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve %s path: %w", name, err)
		}
		info, err := os.Stat(absolute)
		if err != nil || info.IsDir() {
			return fmt.Errorf("%s was not found at %s", name, absolute)
		}
		paths[name] = absolute
	}
	cfg := appconfig.Default()
	cfg.KataGoPath = paths["KataGo"]
	cfg.ModelPath = paths["model"]
	cfg.AnalysisConfig = paths["analysis config"]
	cfg.CloudflaredPath = paths["cloudflared"]
	if options.Listen != "" {
		cfg.Listen = options.Listen
	}
	if options.MaxVisits > 0 {
		cfg.MaxVisits = options.MaxVisits
	}
	if err := appconfig.Save(home, cfg); err != nil {
		return err
	}
	_, err := appconfig.EnsureToken(home, false)
	return err
}

func Run(ctx context.Context, home, version string, logger *log.Logger) error {
	if err := appconfig.EnsurePrivateDirs(home); err != nil {
		return err
	}
	cfg, err := appconfig.Load(home)
	if err != nil {
		return err
	}
	token, err := appconfig.ReadToken(home)
	if err != nil {
		return err
	}
	if err := writeCurrentProcessState(home); err != nil {
		return fmt.Errorf("record service process: %w", err)
	}
	defer removeCurrentProcessState(home)
	defer os.Remove(connectionStatePath(home))

	engine, err := bridge.Start(ctx, bridge.EngineConfig{
		Executable:      cfg.KataGoPath,
		Model:           cfg.ModelPath,
		Config:          cfg.AnalysisConfig,
		WorkingDir:      home,
		Environment:     cfg.ExtraEnvironment,
		MaxVisits:       cfg.MaxVisits,
		StartupTimeout:  cfg.StartupTimeout,
		ShutdownTimeout: 8 * time.Second,
	}, logger)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		_ = engine.Shutdown(context.Background())
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	server, err := bridge.NewServer(engine, token, cfg.MaxMessageBytes, logger)
	if err != nil {
		_ = listener.Close()
		_ = engine.Shutdown(context.Background())
		return err
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(listener)
		close(serverDone)
	}()

	tunnelProcess, baseURL, err := startTunnel(
		ctx,
		cfg.CloudflaredPath,
		cfg.Listen,
		cfg.TunnelTimeout,
		logger,
	)
	if err != nil {
		shutdown(ctx, server, engine, nil)
		return err
	}
	connectionURL, err := buildConnectionURL(baseURL, token)
	if err != nil {
		shutdown(ctx, server, engine, tunnelProcess)
		return err
	}
	state := ConnectionState{
		URL:       connectionURL,
		BaseURL:   baseURL,
		CreatedAt: time.Now().UTC(),
		Version:   version,
	}
	if err := writePrivateJSON(connectionStatePath(home), state); err != nil {
		shutdown(ctx, server, engine, tunnelProcess)
		return fmt.Errorf("save connection link: %w", err)
	}
	if _, err := WriteQR(home, connectionURL); err != nil {
		logger.Printf("QR image could not be written: %v", err)
	}
	logger.Printf("secure tunnel ready at %s (access token omitted)", baseURL)

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-engine.Done():
		if runErr == nil {
			runErr = errors.New("KataGo exited unexpectedly")
		} else {
			runErr = fmt.Errorf("KataGo exited unexpectedly: %w", runErr)
		}
	case runErr = <-serverDone:
		if runErr == nil {
			runErr = errors.New("WebSocket server stopped unexpectedly")
		}
	case runErr = <-tunnelProcess.done:
		if runErr == nil {
			runErr = errors.New("secure tunnel stopped unexpectedly")
		} else {
			runErr = fmt.Errorf("secure tunnel stopped unexpectedly: %w", runErr)
		}
	}
	shutdown(context.Background(), server, engine, tunnelProcess)
	return runErr
}

func shutdown(ctx context.Context, server *bridge.Server, engine *bridge.Engine, tunnelProcess *tunnel) {
	if tunnelProcess != nil {
		tunnelProcess.stop(5 * time.Second)
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if server != nil {
		_ = server.Shutdown(shutdownCtx)
	}
	if engine != nil {
		_ = engine.Shutdown(shutdownCtx)
	}
}

func buildConnectionURL(baseURL, token string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse tunnel URL: %w", err)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	default:
		return "", fmt.Errorf("unsupported tunnel URL scheme %q", parsed.Scheme)
	}
	parsed.Path = "/katago/" + token
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func StartDaemon(home, version string, output io.Writer) error {
	if _, err := appconfig.Load(home); err != nil {
		return err
	}
	if _, err := appconfig.ReadToken(home); err != nil {
		return err
	}
	status, _ := ReadStatus(home)
	if status.Running {
		return Show(home, output)
	}
	if err := appconfig.EnsurePrivateDirs(home); err != nil {
		return err
	}
	_ = os.Remove(connectionStatePath(home))
	_ = os.Remove(processStatePath(home))
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(home, appconfig.LogFileName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open service log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.Command(executable, "run", "--home", home, "--version", version)
	cmd.Dir = home
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	_ = cmd.Process.Release()

	cfg, _ := appconfig.Load(home)
	deadline := time.Now().Add(cfg.StartupTimeout + cfg.TunnelTimeout + 15*time.Second)
	for time.Now().Before(deadline) {
		status, _ := ReadStatus(home)
		if status.Running && status.Connection != nil {
			return Show(home, output)
		}
		if state, err := readProcessState(home); err == nil && !processMatches(state) {
			return fmt.Errorf("service stopped during startup; inspect %s", logPath)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("service startup timed out; inspect %s", logPath)
}

func Stop(home string, output io.Writer) error {
	state, err := readProcessState(home)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(output, "Already stopped / 服务未运行")
			return nil
		}
		return err
	}
	if !processMatches(state) {
		_ = os.Remove(processStatePath(home))
		_ = os.Remove(connectionStatePath(home))
		_, _ = fmt.Fprintln(output, "Already stopped / 服务未运行")
		return nil
	}
	process, err := os.FindProcess(state.PID)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop service process %s: %w", pidDescription(state), err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processMatches(state) {
			_ = os.Remove(processStatePath(home))
			_ = os.Remove(connectionStatePath(home))
			_, _ = fmt.Fprintln(output, "Stopped / 已停止")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	if processMatches(state) {
		_ = process.Kill()
	}
	_ = os.Remove(processStatePath(home))
	_ = os.Remove(connectionStatePath(home))
	_, _ = fmt.Fprintln(output, "Stopped after forced cleanup / 已强制停止")
	return nil
}

func ReadStatus(home string) (Status, error) {
	state, err := readProcessState(home)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Status{}, nil
		}
		return Status{}, err
	}
	status := Status{PID: state.PID, Running: processMatches(state)}
	if !status.Running {
		return status, nil
	}
	data, err := os.ReadFile(connectionStatePath(home))
	if err == nil {
		var connection ConnectionState
		if json.Unmarshal(data, &connection) == nil && strings.HasPrefix(connection.URL, "wss://") {
			status.Connection = &connection
		}
	}
	return status, nil
}

func Show(home string, output io.Writer) error {
	status, err := ReadStatus(home)
	if err != nil {
		return err
	}
	if !status.Running {
		return errors.New("service is not running; run katago-remote start")
	}
	if status.Connection == nil {
		return errors.New("service is still starting; try again shortly")
	}
	_, _ = fmt.Fprintln(output, "")
	_, _ = fmt.Fprintln(output, "Ready / 远程算力已就绪")
	_, _ = fmt.Fprintln(output, "")
	_, _ = fmt.Fprintln(output, status.Connection.URL)
	_, _ = fmt.Fprintln(output, "")
	_ = PrintQR(output, status.Connection.URL)
	_, _ = fmt.Fprintf(output, "\nQR image / 二维码图片: %s\n", filepath.Join(home, "state", "connection-qr.png"))
	_, _ = fmt.Fprintln(output, "Keep this link private. Paste it into LizzieYzy Next > Remote Compute > Custom Compute.")
	_, _ = fmt.Fprintln(output, "请勿公开此链接。将它粘贴到 LizzieYzy Next > 远程算力 > 自建算力。")
	return nil
}

func ResetLink(home, version string, output io.Writer) error {
	if err := Stop(home, io.Discard); err != nil {
		return err
	}
	if _, err := appconfig.EnsureToken(home, true); err != nil {
		return err
	}
	return StartDaemon(home, version, output)
}

func Doctor(ctx context.Context, home string) []Check {
	checks := []Check{
		{Name: "Operating system", OK: runtime.GOOS == "linux", Message: runtime.GOOS + "/" + runtime.GOARCH},
		{Name: "Architecture", OK: runtime.GOARCH == "amd64", Message: runtime.GOARCH},
	}
	cfg, err := appconfig.Load(home)
	if err != nil {
		return append(checks, Check{Name: "Configuration", OK: false, Message: err.Error()})
	}
	checks = append(checks, Check{Name: "Configuration", OK: true, Message: filepath.Join(home, appconfig.ConfigFileName)})
	for name, path := range map[string]string{
		"KataGo":          cfg.KataGoPath,
		"Model":           cfg.ModelPath,
		"Analysis config": cfg.AnalysisConfig,
		"cloudflared":     cfg.CloudflaredPath,
	} {
		info, statErr := os.Stat(path)
		checks = append(checks, Check{Name: name, OK: statErr == nil && !infoIsDirectory(info), Message: path})
	}
	checks = append(checks, commandCheck(ctx, "NVIDIA driver", "nvidia-smi", "--query-gpu=name,driver_version,memory.total", "--format=csv,noheader"))
	checks = append(checks, commandCheckWithEnv(ctx, "KataGo startup", cfg.ExtraEnvironment, cfg.KataGoPath, "version"))
	return checks
}

func infoIsDirectory(info os.FileInfo) bool {
	return info != nil && info.IsDir()
}

func commandCheck(ctx context.Context, name, command string, args ...string) Check {
	return commandCheckWithEnv(ctx, name, nil, command, args...)
}

func commandCheckWithEnv(ctx context.Context, name string, environment []string, command string, args ...string) Check {
	commandCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, command, args...)
	cmd.Env = append(os.Environ(), environment...)
	data, err := cmd.CombinedOutput()
	message := strings.TrimSpace(string(data))
	if len(message) > 500 {
		message = message[:500] + "..."
	}
	if err != nil {
		if message == "" {
			message = err.Error()
		}
		return Check{Name: name, OK: false, Message: message}
	}
	if message == "" {
		message = "OK"
	}
	return Check{Name: name, OK: true, Message: message}
}

func TailLog(home string, output io.Writer, lines int) error {
	if lines <= 0 {
		lines = 80
	}
	file, err := os.Open(filepath.Join(home, appconfig.LogFileName))
	if err != nil {
		return err
	}
	defer file.Close()
	buffer := make([]string, lines)
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		buffer[count%lines] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	start := 0
	if count > lines {
		start = count % lines
	}
	limit := count
	if limit > lines {
		limit = lines
	}
	for i := 0; i < limit; i++ {
		_, _ = fmt.Fprintln(output, buffer[(start+i)%lines])
	}
	return nil
}
