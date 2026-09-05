package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
