//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

// New returns the tree control for this platform. Process groups are always
// available on POSIX, so useJob is ignored.
func New(_ bool) (TreeControl, error) { return &posixTree{}, nil }

// posixTree runs each child in its own process group so cancelation can signal
// the whole tree instead of only the direct child.
type posixTree struct{}

func (t *posixTree) SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func (t *posixTree) Attach(*exec.Cmd) error { return nil }

func (t *posixTree) Graceful(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = signalGroup(cmd.Process.Pid, syscall.SIGTERM)
}

func (t *posixTree) Force(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = signalGroup(cmd.Process.Pid, syscall.SIGKILL)
}

func (t *posixTree) Release() {}

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
