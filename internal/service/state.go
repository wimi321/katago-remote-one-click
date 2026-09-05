package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	appconfig "github.com/wimi321/katago-remote-one-click/internal/config"
)

type ProcessState struct {
	PID        int       `json:"pid"`
	StartToken string    `json:"startToken,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
}

type ConnectionState struct {
	URL       string    `json:"url"`
	BaseURL   string    `json:"baseUrl"`
	CreatedAt time.Time `json:"createdAt"`
	Version   string    `json:"version"`
}

func processStatePath(home string) string {
	return filepath.Join(home, appconfig.PIDFileName)
}

func connectionStatePath(home string) string {
	return filepath.Join(home, appconfig.ConnectionFileName)
}

func readProcessState(home string) (ProcessState, error) {
	var state ProcessState
	data, err := os.ReadFile(processStatePath(home))
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func writePrivateJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func currentStartToken(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	line := string(data)
	closing := strings.LastIndex(line, ")")
	if closing < 0 || closing+2 >= len(line) {
		return ""
	}
	fields := strings.Fields(line[closing+2:])
	// /proc stat field 22 is process start time; fields starts at field 3.
	if len(fields) <= 19 {
		return ""
	}
	return fields[19]
}

func processMatches(state ProcessState) bool {
	if state.PID <= 1 {
		return false
	}
	if err := syscall.Kill(state.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	if state.StartToken != "" {
		return currentStartToken(state.PID) == state.StartToken
	}
	return true
}

func writeCurrentProcessState(home string) error {
	pid := os.Getpid()
	return writePrivateJSON(processStatePath(home), ProcessState{
		PID:        pid,
		StartToken: currentStartToken(pid),
		StartedAt:  time.Now().UTC(),
	})
}

func removeCurrentProcessState(home string) {
	state, err := readProcessState(home)
	if err == nil && state.PID == os.Getpid() {
		_ = os.Remove(processStatePath(home))
	}
}

func pidDescription(state ProcessState) string {
	return strconv.Itoa(state.PID)
}
