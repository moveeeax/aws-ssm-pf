//go:build !windows

package session

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcessGroup makes the child the leader of a new process group so that the
// aws CLI and the session-manager-plugin it forks can be signalled together.
//
// Safe for this tool specifically: the only documents it can drive are
// port-forwarding ones (it always passes portNumber/localPortNumber), and
// session-manager-plugin's port handler never reads the terminal, so moving the
// child out of the foreground process group cannot deadlock it on SIGTTIN.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalProcessGroup sends sig to every process in the group led by proc. A nil
// or already-reaped process is a no-op.
func signalProcessGroup(proc *os.Process, sig os.Signal) error {
	if proc == nil || proc.Pid <= 0 {
		return nil
	}
	s, ok := sig.(syscall.Signal)
	if !ok {
		return proc.Signal(sig)
	}
	// Negative pid addresses the whole process group; the child is its leader,
	// so its pid doubles as the pgid.
	if err := syscall.Kill(-proc.Pid, s); err != nil {
		if err == syscall.ESRCH {
			return nil // already gone
		}
		return err
	}
	return nil
}
