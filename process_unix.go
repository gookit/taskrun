//go:build !windows

package kscript

import (
	"os/exec"
	"syscall"
)

// syscallSysProcAttr aliases the platform process attributes.
type syscallSysProcAttr = syscall.SysProcAttr

// posixTree runs each child in its own process group so cancelation can signal
// the whole tree instead of only the direct child.
type posixTree struct{}

func newTreeControl() (treeControl, error) { return &posixTree{}, nil }

func (t *posixTree) sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func (t *posixTree) attach(cmd *exec.Cmd) error { return nil }

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
