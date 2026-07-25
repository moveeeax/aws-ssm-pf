//go:build windows

package session

import (
	"errors"
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on Windows: there is no portable equivalent of
// setpgid, and bounding the whole tree would need a job object. The aws CLI is
// still terminated on cancellation, only without the process-group guarantee.
func setProcessGroup(*exec.Cmd) {}

// signalProcessGroup falls back to terminating just the child process. Windows
// cannot deliver SIGINT to another process, so both the graceful and the forced
// path end up as a kill.
func signalProcessGroup(proc *os.Process, _ os.Signal) error {
	if proc == nil || proc.Pid <= 0 {
		return nil
	}
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
