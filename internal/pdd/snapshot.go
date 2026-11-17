package pdd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Snapshot struct {
	Hash  string
	Files map[string][]byte
	Modes map[string]fs.FileMode
}

// Capture includes tracked and untracked files. Build outputs and PDD receipts
// are excluded so a proof does not invalidate itself by writing its own log.
func Capture(root string) (Snapshot, error) {
	s := Snapshot{Files: map[string][]byte{}, Modes: map[string]fs.FileMode{}}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		first := strings.Split(filepath.ToSlash(rel), "/")[0]
		if first == ".git" || first == ".pdd" || first == "bin" || first == "dist" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot refuses symlink %s", rel)
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file: %s", rel)
		}
		if info.Size() > 16<<20 {
			return fmt.Errorf("snapshot file exceeds 16 MiB: %s", rel)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s.Files[filepath.ToSlash(rel)] = b
		s.Modes[filepath.ToSlash(rel)] = info.Mode().Perm()
		return nil
	})
	if err != nil {
		return s, err
	}
	var names []string
	for n := range s.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%d:%s:%o:%d:", len(n), n, s.Modes[n], len(s.Files[n]))
		h.Write(s.Files[n])
	}
	s.Hash = hex.EncodeToString(h.Sum(nil))
	return s, nil
}
func (s Snapshot) Materialize(root string) error {
	for path, b := range s.Files {
		dest := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, b, s.Modes[path]); err != nil {
			return err
		}
	}
	return nil
}
