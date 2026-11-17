package pdd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

var processKernel = syscall.NewLazyDLL("kernel32.dll")
var (
	createJobObject   = processKernel.NewProc("CreateJobObjectW")
	setJobInformation = processKernel.NewProc("SetInformationJobObject")
	assignJobProcess  = processKernel.NewProc("AssignProcessToJobObject")
	terminateJob      = processKernel.NewProc("TerminateJobObject")
	firstThread       = processKernel.NewProc("Thread32First")
	nextThread        = processKernel.NewProc("Thread32Next")
	openThread        = processKernel.NewProc("OpenThread")
	resumeThread      = processKernel.NewProc("ResumeThread")
)

type jobBasicLimits struct {
	ProcessTime     int64
	JobTime         int64
	Flags           uint32
	MinWorkingSet   uintptr
	MaxWorkingSet   uintptr
	ActiveProcesses uint32
	Affinity        uintptr
	Priority        uint32
	Scheduling      uint32
}
type jobExtendedLimits struct {
	Basic             jobBasicLimits
	IO                [6]uint64
	ProcessMemory     uintptr
	JobMemory         uintptr
	PeakProcessMemory uintptr
	PeakJobMemory     uintptr
}
type threadEntry struct {
	Size          uint32
	Usage         uint32
	ID            uint32
	ProcessID     uint32
	BasePriority  int32
	DeltaPriority int32
	Flags         uint32
}

func configureProcess(cmd *exec.Cmd, ctx context.Context) (processControl, error) {
	var limits jobExtendedLimits
	if unsafe.Sizeof(limits) != 144 {
		return processControl{}, fmt.Errorf("Windows process containment requires a 64-bit host")
	}
	raw, _, err := createJobObject.Call(0, 0)
	if raw == 0 {
		return processControl{}, fmt.Errorf("create process job: %w", err)
	}
	job := syscall.Handle(raw)
	// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE; the handle is not inheritable.
	limits.Basic.Flags = 0x2000
	ok, _, err := setJobInformation.Call(uintptr(job), 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		syscall.CloseHandle(job)
		return processControl{}, fmt.Errorf("configure process job: %w", err)
	}
	var mu sync.Mutex
	cleanup := func(error) {
		mu.Lock()
		defer mu.Unlock()
		if job != 0 {
			_ = syscall.CloseHandle(job)
			job = 0
		}
	}
	// CREATE_SUSPENDED prevents child code from spawning descendants before the
	// root process has been assigned to the job. Never fall back to an uncontained run.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000004}
	cmd.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if job == 0 {
			return os.ErrProcessDone
		}
		ok, _, err := terminateJob.Call(uintptr(job), 1)
		if ok == 0 {
			return fmt.Errorf("terminate process job: %w", err)
		}
		return nil
	}
	start := func() error {
		process, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(cmd.Process.Pid))
		if err != nil {
			return fmt.Errorf("open suspended process: %w", err)
		}
		defer syscall.CloseHandle(process)
		ok, _, err := assignJobProcess.Call(uintptr(job), uintptr(process))
		if ok == 0 {
			return fmt.Errorf("assign process job: %w", err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		return resumeInitialThread(uint32(cmd.Process.Pid))
	}
	return processControl{start: start, cleanup: cleanup}, nil
}

// os/exec closes the primary thread handle returned by CreateProcess. Recover
// that still-suspended thread using the documented Toolhelp API. Unexpected
// thread layouts fail closed rather than running outside containment.
func resumeInitialThread(pid uint32) error {
	snapshot, err := syscall.CreateToolhelp32Snapshot(0x00000004, 0)
	if err != nil {
		return fmt.Errorf("snapshot suspended thread: %w", err)
	}
	defer syscall.CloseHandle(snapshot)
	entry := threadEntry{Size: uint32(unsafe.Sizeof(threadEntry{}))}
	ok, _, err := firstThread.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
	if ok == 0 {
		return fmt.Errorf("enumerate suspended thread: %w", err)
	}
	var threads []uint32
	for {
		if entry.ProcessID == pid {
			threads = append(threads, entry.ID)
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, err = nextThread.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
		if ok == 0 {
			if err != syscall.ERROR_NO_MORE_FILES {
				return fmt.Errorf("enumerate suspended thread: %w", err)
			}
			break
		}
	}
	if len(threads) != 1 {
		return fmt.Errorf("expected one initial suspended thread, found %d", len(threads))
	}
	raw, _, err := openThread.Call(0x0002, 0, uintptr(threads[0]))
	if raw == 0 {
		return fmt.Errorf("open suspended thread: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(raw))
	previous, _, err := resumeThread.Call(raw)
	if uint32(previous) == ^uint32(0) {
		return fmt.Errorf("resume suspended thread: %w", err)
	}
	if previous != 1 {
		return fmt.Errorf("unexpected initial thread suspend count: %d", previous)
	}
	return nil
}
