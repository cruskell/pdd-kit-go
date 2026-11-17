//go:build !windows

package pdd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func processRunning(pid int) bool {
	if syscall.Kill(pid, 0) == syscall.ESRCH {
		return false
	}
	if b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
		tail := string(b[strings.LastIndex(string(b), ")")+1:])
		if strings.HasPrefix(strings.TrimSpace(tail), "Z ") {
			return false
		}
	}
	return true
}
