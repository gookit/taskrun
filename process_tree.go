package taskrun

import "os/exec"

// treeControl owns the platform specific child process tree of one action.
type treeControl interface {
	// sysProcAttr returns the attributes the child must be created with.
	sysProcAttr() *syscallSysProcAttr
	// attach binds the started process to the tree, so descendants are owned.
	attach(cmd *exec.Cmd) error
	// graceful asks the tree to stop.
	graceful(cmd *exec.Cmd)
	// force terminates the tree.
	force(cmd *exec.Cmd)
	// release frees platform resources.
	release()
}
