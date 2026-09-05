package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var quickTunnelURL = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

type tunnelStartupEvent struct {
	url        string
	registered bool
}

type tunnel struct {
	cmd  *exec.Cmd
	done chan error
	once sync.Once
}

func startTunnel(
	ctx context.Context,
	executable string,
	localAddress string,
	timeout time.Duration,
	logger *log.Logger,
) (*tunnel, string, error) {
	cmd := exec.CommandContext(
		ctx,
		executable,
		"tunnel",
		"--no-autoupdate",
		"--url",
		"http://"+localAddress,
		"--protocol",
		"http2",
		"--loglevel",
		"info",
	)
	configureChildProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, "", err
	}
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("start cloudflared: %w", err)
	}
	t := &tunnel{cmd: cmd, done: make(chan error, 1)}
	go func() {
		err := cmd.Wait()
		t.done <- err
		close(t.done)
	}()

	events := make(chan tunnelStartupEvent, 32)
	read := func(prefix string, input io.Reader) {
		scanner := bufio.NewScanner(input)
		scanner.Buffer(make([]byte, 16*1024), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			logger.Printf("%s: %s", prefix, line)
			event := tunnelStartupEvent{
				url:        quickTunnelURL.FindString(line),
				registered: strings.Contains(line, "Registered tunnel connection"),
			}
			if event.url != "" || event.registered {
				select {
				case events <- event:
				default:
				}
			}
		}
	}
	go read("cloudflared", stdout)
	go read("cloudflared", stderr)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var baseURL string
	registered := false
	for baseURL == "" || !registered {
		select {
		case event := <-events:
			if event.url != "" {
				baseURL = event.url
			}
			registered = registered || event.registered
		case err := <-t.done:
			if err == nil {
				err = errors.New("cloudflared exited before the tunnel became ready")
			}
			return nil, "", err
		case <-timer.C:
			t.stop(5 * time.Second)
			return nil, "", errors.New("timed out waiting for the secure tunnel to register")
		case <-ctx.Done():
			t.stop(5 * time.Second)
			return nil, "", ctx.Err()
		}
	}
	return t, baseURL, nil
}

func (t *tunnel) stop(timeout time.Duration) {
	if t == nil {
		return
	}
	t.once.Do(func() {
		terminateChild(t.cmd)
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-t.done:
		case <-timer.C:
			killChild(t.cmd)
			<-t.done
		}
	})
}
