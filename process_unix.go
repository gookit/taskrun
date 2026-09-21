//go:build !windows

package taskrun

import (
	"os/exec"
	"syscall"
)

// syscallSysProcAttr aliases the platform process attributes.
type syscallSysProcAttr = syscall.SysProcAttr

// posixTree runs each child in its own process group so cancelation can signal
// the whole tree instead of only the direct child. Process groups are always
// available, so the job flag is ignored.
type posixTree struct{}

func newTreeControl(_ bool) (treeControl, error) { return &posixTree{}, nil }

func (t *posixTree) sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func (t *posixTree) attach(*exec.Cmd) error { return nil }

func (t *posixTree) graceful(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = signalGroup(cmd.Process.Pid, syscall.SIGTERM)
}

func (t *posixTree) force(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = signalGroup(cmd.Process.Pid, syscall.SIGKILL)
}

func (t *posixTree) release() {}

// signalGroup signals the process group led by pid, which owns every
// descendant the action started.
func signalGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, signal); err != nil {
		return syscall.Kill(pid, signal)
	}
	return nil
}
