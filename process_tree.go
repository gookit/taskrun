package taskrun

import "os/exec"

// TreeKillMode selects what the default engine does when the strongest
// available tree cleanup cannot be established.
type TreeKillMode int

const (
	// TreeKillAuto falls back to the next available mechanism. On Windows, when
	// the process cannot be assigned to a job object (for example because a
	// restricted job already owns it, as on GitHub Actions runners), the engine
	// terminates the tree by walking parent process ids instead. The tree is
	// still terminated; only the owning mechanism differs.
	TreeKillAuto TreeKillMode = iota
	// TreeKillRequired fails the action instead of falling back. Use it when the
	// strongest platform guarantee is mandatory.
	TreeKillRequired
)

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
