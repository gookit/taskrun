// Package process owns the platform specific process tree of one action.
//
// On POSIX every child leads its own process group, so cancelation can signal
// the whole tree. On Windows children join a job object; when the host refuses a
// nested assignment (a restricted job already owns the process, as on CI
// runners) the tree is terminated by walking live parent process ids instead.
package process

import (
	"os/exec"
	"syscall"
)

// TreeControl owns the child process tree of one action. The engine creates the
// strongest available control before starting the process, so a missing
// capability can fail the action before anything is launched.
type TreeControl interface {
	// SysProcAttr returns the attributes the child must be created with.
	SysProcAttr() *syscall.SysProcAttr
	// Attach binds the started process to the tree, so descendants are owned.
	Attach(cmd *exec.Cmd) error
	// Graceful asks the tree to stop.
	Graceful(cmd *exec.Cmd)
	// Force terminates the tree.
	Force(cmd *exec.Cmd)
	// Release frees platform resources. It must be called exactly once per
	// created control.
	Release()
}
