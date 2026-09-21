//go:build windows

package taskrun

import (
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

// syscallSysProcAttr aliases the platform process attributes.
type syscallSysProcAttr = syscall.SysProcAttr

// Windows Job Objects let the engine own an entire process tree and kill it as
// a unit. The job is created before the child starts, so a missing capability
// fails the action before any process is launched.
var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	procGenerateConsoleCtrlEvent = kernel32.NewProc("GenerateConsoleCtrlEvent")
)

const (
	jobObjectExtendedLimitInfo   = 9
	jobObjectLimitKillOnJobClose = 0x00002000
	createNewProcessGroup        = 0x00000200
	ctrlBreakEvent               = 1
	processTerminate             = 0x0001
	processSetQuota              = 0x0100
)

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// windowsTree owns one job object.
type windowsTree struct {
	handle syscall.Handle
}

// newTreeControl builds the strongest available tree controller. When useJob is
// false it returns the parent-chain controller directly, which is what the
// engine falls back to in TreeKillAuto mode.
func newTreeControl(useJob bool) (treeControl, error) {
	if !useJob {
		return &windowsTaskkillTree{}, nil
	}
	handle, _, err := procCreateJobObjectW.Call(0, 0)
	if handle == 0 {
		return nil, errf("CreateJobObject failed: %v", err)
	}
	job := syscall.Handle(handle)
	info := jobObjectExtendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	result, _, err := procSetInformationJobObject.Call(
		uintptr(job),
		uintptr(jobObjectExtendedLimitInfo),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if result == 0 {
		_ = syscall.CloseHandle(job)
		return nil, errf("SetInformationJobObject failed: %v", err)
	}
	return &windowsTree{handle: job}, nil
}

// windowsTaskkillTree terminates the tree by walking live parent process ids
// with taskkill. It is the fallback for environments where the process cannot be
// assigned to a job object, for example when a restricted job already owns it on
// CI runners. The whole tree is still terminated.
type windowsTaskkillTree struct{}

func (t *windowsTaskkillTree) sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func (t *windowsTaskkillTree) attach(*exec.Cmd) error { return nil }

func (t *windowsTaskkillTree) graceful(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_, _, _ = procGenerateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(cmd.Process.Pid))
}

func (t *windowsTaskkillTree) force(cmd *exec.Cmd) { taskkillTree(cmd) }

func (t *windowsTaskkillTree) release() {}

// taskkillTree kills a process and its descendants through their live parent
// chain.
func taskkillTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = kill.Run()
}

func (t *windowsTree) sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func (t *windowsTree) attach(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return errf("process is not started")
	}
	process, _, err := procOpenProcess.Call(
		uintptr(processTerminate|processSetQuota),
		0,
		uintptr(cmd.Process.Pid),
	)
	if process == 0 {
		return errf("OpenProcess for pid %d failed: %v", cmd.Process.Pid, err)
	}
	defer syscall.CloseHandle(syscall.Handle(process))
	result, _, err := procAssignProcessToJobObject.Call(uintptr(t.handle), process)
	if result == 0 {
		return errf("AssignProcessToJobObject for pid %d failed: %v", cmd.Process.Pid, err)
	}
	return nil
}

func (t *windowsTree) graceful(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Best effort: only works for children sharing the console.
	_, _, _ = procGenerateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(cmd.Process.Pid))
}

func (t *windowsTree) force(cmd *exec.Cmd) {
	if t.handle != 0 {
		_, _, _ = procTerminateJobObject.Call(uintptr(t.handle), 1)
	}
	// Belt and braces: a descendant created in the short window between
	// CreateProcess and AssignProcessToJobObject is not owned by the job, so the
	// tree is also killed by walking parent process ids. taskkill walks the live
	// parent chain, which still contains such a descendant while the direct child
	// is alive.
	taskkillTree(cmd)
}

func (t *windowsTree) release() {
	if t.handle == 0 {
		return
	}
	_ = syscall.CloseHandle(t.handle)
	t.handle = 0
}
