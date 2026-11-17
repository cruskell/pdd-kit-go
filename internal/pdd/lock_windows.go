package pdd

import (
	"os"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func lockFile(f *os.File) error {
	var offset syscall.Overlapped
	result, _, err := lockFileEx.Call(uintptr(f.Fd()), 3, 0, 1, 0, uintptr(unsafe.Pointer(&offset)))
	if result == 0 {
		return err
	}
	return nil
}
