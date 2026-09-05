package bridge

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketServerAuthenticationAndSingleClient(t *testing.T) {
	engine := startFakeEngine(t)
	token := strings.Repeat("a", 43)
	server, err := NewServer(engine, token, 1<<20, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	base := "ws://" + listener.Addr().String()

	bad, response, err := websocket.Dial(context.Background(), base+"/katago/wrong", nil)
	if bad != nil {
		_ = bad.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("bad token response = %#v, err = %v", response, err)
	}

	first, _, err := websocket.Dial(context.Background(), base+"/katago/"+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.CloseNow()
	second, _, err := websocket.Dial(context.Background(), base+"/katago/"+token, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, secondErr := second.Read(context.Background())
	if websocket.CloseStatus(secondErr) != websocket.StatusTryAgainLater {
		t.Fatalf("second client close status = %d, err = %v", websocket.CloseStatus(secondErr), secondErr)
	}

	if err := first.Write(context.Background(), websocket.MessageText, []byte(`{"id":"live"}`)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := first.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"id":"live"`) || strings.Contains(string(payload), "remote_") {
		t.Fatalf("unexpected response: %s", payload)
	}
}
