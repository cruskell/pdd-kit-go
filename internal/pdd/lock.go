package pdd

import (
	"fmt"
	"os"
	"path/filepath"
)

// Lock files remain in place: unlinking a locked inode could let a second
// process lock a different inode under the same name. The OS releases the lock
// when the descriptor closes or its process exits, including after a crash.
func lockProject(root string) (func(), error) {
	path := filepath.Join(root, ".pdd", "run.lock")
	if err := NoSymlinks(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("another proof, approval or status operation may be using this project: %w", err)
	}
	return func() { _ = f.Close() }, nil
}
