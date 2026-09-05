//go:build linux

package service

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

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
	return state.StartToken == "" || currentStartToken(state.PID) == state.StartToken
}
