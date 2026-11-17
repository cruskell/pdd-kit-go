package pdd

import (
	"context"
	"fmt"
	"os"
)

func readArtifact(path string, limit int64) ([]byte, error) {
	if err := NoSymlinks(path); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact is not a regular file: %s", path)
	}
	if before.Size() > limit {
		return nil, fmt.Errorf("artifact exceeds %d bytes: %s", limit, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !current.Mode().IsRegular() || !os.SameFile(before, current) {
		return nil, fmt.Errorf("artifact changed while opening: %s", path)
	}
	return readInput(context.Background(), f, limit)
}
