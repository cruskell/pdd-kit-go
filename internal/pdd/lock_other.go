//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package pdd

import (
	"fmt"
	"os"
)

func lockFile(*os.File) error { return fmt.Errorf("project locking is not supported on this platform") }
