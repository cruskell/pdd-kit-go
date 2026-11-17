//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package pdd

import (
	"context"
	"os/exec"
)

func configureProcess(*exec.Cmd, context.Context) (processControl, error) {
	return processControl{start: func() error { return nil }, cleanup: func(error) {}}, nil
}
