//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package pdd

import "os/exec"

func configureProcess(*exec.Cmd)      {}
func cleanupProcess(*exec.Cmd, error) {}
