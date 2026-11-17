//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package pdd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd, _ context.Context) (processControl, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return processControl{start: func() error { return nil }, cleanup: func(error) {
		// Descendants are scoped to one check, even when the parent has exited and
		// they no longer hold its output pipes. Clean up on every completion path.
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}}, nil
}
