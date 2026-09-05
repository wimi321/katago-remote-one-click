//go:build !linux && !darwin

package service

import "os/exec"

func detachProcess(cmd *exec.Cmd)         {}
func configureChildProcess(cmd *exec.Cmd) {}

func terminateChild(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func killChild(cmd *exec.Cmd) {
	terminateChild(cmd)
}
