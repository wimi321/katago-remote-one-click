package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/wimi321/katago-remote-one-click/internal/service"
)

func TestPublicQuickTunnelRoundTrip(t *testing.T) {
	if os.Getenv("KATAGO_REMOTE_PUBLIC_E2E") != "1" {
		t.Skip("set KATAGO_REMOTE_PUBLIC_E2E=1 to exercise a real Cloudflare Quick Tunnel")
	}
	cloudflared := os.Getenv("KATAGO_REMOTE_CLOUDFLARED")
	if cloudflared == "" {
		t.Fatal("KATAGO_REMOTE_CLOUDFLARED must name the pinned cloudflared executable")
	}

	home := t.TempDir()
	fakeKataGo := writeFakeKataGoLauncher(t, home)
	model := writeFile(t, home, "model.bin.gz", "integration model\n", 0o600)
	analysisConfig := writeFile(t, home, "analysis.cfg", "logDir = logs\n", 0o600)
	listen := reserveAddress(t)
	if err := service.Initialize(home, service.InitOptions{
		KataGo:      fakeKataGo,
		Model:       model,
		Config:      analysisConfig,
		Cloudflared: cloudflared,
		Listen:      listen,
		MaxVisits:   10_000,
	}); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx, home, "integration", log.New(&logs, "", log.LstdFlags))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("service shutdown: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("service did not stop within 15 seconds")
		}
		if t.Failed() {
			t.Logf("sanitized service log:\n%s", redactConnectionPaths(logs.String()))
		}
	})

	connectionURL := waitForConnection(t, home, 75*time.Second)
	t.Logf("probing public origin %s (token omitted)", publicOrigin(connectionURL))
	waitForPublicHealth(t, connectionURL, 60*time.Second)
	connection := dialWithRetry(t, connectionURL, 60*time.Second)
	defer connection.CloseNow()

	query := `{"id":"public-round-trip","boardXSize":19,"boardYSize":19,"rules":"Chinese","komi":7.5,"maxVisits":32}`
	requestCtx, requestCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer requestCancel()
	if err := connection.Write(requestCtx, websocket.MessageText, []byte(query)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := connection.Read(requestCtx)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("decode public response: %v (%s)", err, payload)
	}
	if response["id"] != "public-round-trip" {
		t.Fatalf("response id = %#v", response["id"])
	}
	if strings.Contains(string(payload), "remote_") {
		t.Fatalf("internal query id leaked: %s", payload)
	}

	wrongURL := connectionURL[:len(connectionURL)-1] + string(differentLastByte(connectionURL[len(connectionURL)-1]))
	wrongTokenClient := publicHTTPClient(15 * time.Second)
	defer wrongTokenClient.CloseIdleConnections()
	bad, httpResponse, dialErr := websocket.Dial(
		context.Background(),
		wrongURL,
		&websocket.DialOptions{HTTPClient: wrongTokenClient},
	)
	if bad != nil {
		_ = bad.CloseNow()
	}
	if httpResponse != nil && httpResponse.Body != nil {
		_ = httpResponse.Body.Close()
	}
	if dialErr == nil || httpResponse == nil || httpResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong token was not rejected: response=%v error=%v", httpResponse, dialErr)
	}

	if strings.Contains(logs.String(), connectionURL) || strings.Contains(logs.String(), "/katago/") {
		t.Fatal("private connection URL leaked into service logs")
	}
	t.Logf("public WSS round trip passed via %s (token omitted)", publicOrigin(connectionURL))
}

func TestPublicHTTPProbe(t *testing.T) {
	target := os.Getenv("KATAGO_REMOTE_PROBE_URL")
	if target == "" {
		t.Skip("set KATAGO_REMOTE_PROBE_URL to probe an existing public endpoint")
	}
	client := publicHTTPClient(15 * time.Second)
	defer client.CloseIdleConnections()
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 500 {
		t.Fatalf("unexpected status: %s", response.Status)
	}
}

func waitForPublicHealth(t *testing.T, connectionURL string, timeout time.Duration) {
	t.Helper()
	origin := publicOrigin(connectionURL)
	client := publicHTTPClient(8 * time.Second)
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(timeout)
	var lastStatus string
	for time.Now().Before(deadline) {
		response, err := client.Get(strings.Replace(origin, "wss://", "https://", 1) + "/healthz")
		if err == nil {
			lastStatus = response.Status
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		} else {
			lastStatus = err.Error()
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("public tunnel health check did not become ready: %s", lastStatus)
}

func dialWithRetry(t *testing.T, url string, timeout time.Duration) *websocket.Conn {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	client := publicHTTPClient(0)
	defer client.CloseIdleConnections()
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		connection, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: client})
		cancel()
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err == nil {
			return connection
		}
		lastErr = err
		time.Sleep(time.Second)
	}
	t.Fatalf("public WSS connection did not become ready: %v", lastErr)
	return nil
}

func publicHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, _, address string) (net.Conn, error) {
		// Force IPv4 so the integration test remains reliable with macOS VPNs that expose
		// synthetic IPv6 DNS answers without providing an IPv6 forwarding path.
		return dialer.DialContext(ctx, "tcp4", address)
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

func waitForConnection(t *testing.T, home string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.ReadStatus(home)
		if err == nil && status.Connection != nil {
			return status.Connection.URL
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("service did not create a public WSS link before the timeout")
	return ""
}

func reserveAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func writeFakeKataGoLauncher(t *testing.T, directory string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf("#!/bin/sh\nexec %q -test.run=TestFakeKataGoProcess -- \"$@\"\n", executable)
	return writeFile(t, directory, "fake-katago", contents, 0o700)
}

func writeFile(t *testing.T, directory, name, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func differentLastByte(value byte) byte {
	if value == 'a' {
		return 'b'
	}
	return 'a'
}

func publicOrigin(value string) string {
	if marker := strings.Index(value, "/katago/"); marker >= 0 {
		return value[:marker]
	}
	return "redacted"
}

func redactConnectionPaths(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		if marker := strings.Index(line, "/katago/"); marker >= 0 {
			lines[index] = line[:marker] + "/katago/<redacted>"
		}
	}
	return strings.Join(lines, "\n")
}

func TestFakeKataGoProcess(t *testing.T) {
	if os.Getenv("KATAGO_REMOTE_PUBLIC_E2E") != "1" {
		return
	}
	var writeMu sync.Mutex
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
		id, _ := query["id"].(string)
		action, _ := query["action"].(string)
		if action == "query_version" {
			write(map[string]any{"id": id, "action": action, "version": "integration"})
			continue
		}
		write(map[string]any{
			"id":             id,
			"turnNumber":     0,
			"isDuringSearch": false,
			"rootInfo":       map[string]any{"visits": 32, "winrate": 0.5},
			"moveInfos":      []any{},
		})
	}
}
