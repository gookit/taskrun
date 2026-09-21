//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

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

// New builds the strongest available tree control. When useJob is false it
// returns the parent-chain control directly, which is what the engine falls back
// to when the owning mechanism is unavailable.
func New(useJob bool) (TreeControl, error) {
	if !useJob {
		return &taskkillTree{}, nil
	}
	handle, _, err := procCreateJobObjectW.Call(0, 0)
	if handle == 0 {
		return nil, fmt.Errorf("CreateJobObject failed: %v", err)
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
		return nil, fmt.Errorf("SetInformationJobObject failed: %v", err)
	}
	return &jobTree{handle: job}, nil
}

// jobTree owns one job object.
type jobTree struct {
	handle syscall.Handle
}

func (t *jobTree) SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func (t *jobTree) Attach(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("process is not started")
	}
	process, _, err := procOpenProcess.Call(
		uintptr(processTerminate|processSetQuota),
		0,
		uintptr(cmd.Process.Pid),
	)
	if process == 0 {
		return fmt.Errorf("OpenProcess for pid %d failed: %v", cmd.Process.Pid, err)
	}
	defer syscall.CloseHandle(syscall.Handle(process))
	result, _, err := procAssignProcessToJobObject.Call(uintptr(t.handle), process)
	if result == 0 {
		return fmt.Errorf("AssignProcessToJobObject for pid %d failed: %v", cmd.Process.Pid, err)
	}
	return nil
}

func (t *jobTree) Graceful(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Best effort: only works for children sharing the console.
	_, _, _ = procGenerateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(cmd.Process.Pid))
}

func (t *jobTree) Force(cmd *exec.Cmd) {
	if t.handle != 0 {
		_, _, _ = procTerminateJobObject.Call(uintptr(t.handle), 1)
	}
	// Belt and braces: a descendant created in the short window between
	// CreateProcess and AssignProcessToJobObject is not owned by the job, so the
	// tree is also killed by walking parent process ids. taskkill walks the live
	// parent chain, which still contains such a descendant while the direct child
	// is alive.
	killTree(cmd)
}

func (t *jobTree) Release() {
	if t.handle == 0 {
		return
	}
	_ = syscall.CloseHandle(t.handle)
	t.handle = 0
}

// taskkillTree terminates the tree by walking live parent process ids with
// taskkill. It is the fallback for environments where the process cannot be
// assigned to a job object, for example when a restricted job already owns it on
// CI runners. The whole tree is still terminated.
type taskkillTree struct{}

func (t *taskkillTree) SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func (t *taskkillTree) Attach(*exec.Cmd) error { return nil }

func (t *taskkillTree) Graceful(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_, _, _ = procGenerateConsoleCtrlEvent.Call(uintptr(ctrlBreakEvent), uintptr(cmd.Process.Pid))
}

func (t *taskkillTree) Force(cmd *exec.Cmd) { killTree(cmd) }

func (t *taskkillTree) Release() {}

// killTree kills a process and its descendants through their live parent chain.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = kill.Run()
}
