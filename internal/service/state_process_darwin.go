//go:build darwin

package service

import (
	"errors"
	"syscall"
)

func currentStartToken(_ int) string {
	return ""
}

func processMatches(state ProcessState) bool {
	if state.PID <= 1 {
		return false
	}
	err := syscall.Kill(state.PID, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
