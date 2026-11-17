package pdd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type InputEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Bytes  int64  `json:"bytes,omitempty"`
	Mode   uint32 `json:"mode"`
	SHA256 string `json:"sha256,omitempty"`
}
type InputManifest struct {
	Schema    int          `json:"schema"`
	Algorithm string       `json:"algorithm"`
	Entries   []InputEntry `json:"entries"`
}
type InputRecord struct {
	Algorithm      string      `json:"algorithm"`
	Files          int         `json:"files"`
	Directories    int         `json:"directories"`
	Bytes          int64       `json:"bytes"`
	Policy         InputPolicy `json:"policy"`
	Manifest       string      `json:"manifest"`
	ManifestSHA256 string      `json:"manifest_sha256"`
}
type Snapshot struct {
	Hash        string
	Files       map[string][]byte
	Modes       map[string]fs.FileMode
	Directories map[string]fs.FileMode
	Manifest    InputManifest
	Policy      InputPolicy
	Bytes       int64
}

func Capture(root string) (Snapshot, error) { return CaptureContext(context.Background(), root, nil) }
func CaptureContext(ctx context.Context, root string, policy *InputPolicy) (Snapshot, error) {
	effective, err := effectivePolicy(policy)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Files: map[string][]byte{}, Modes: map[string]fs.FileMode{}, Directories: map[string]fs.FileMode{}, Policy: effective, Manifest: InputManifest{Schema: 1, Algorithm: "pdd-inputs-v2", Entries: []InputEntry{}}}
	if err = NoSymlinks(root); err != nil {
		return s, err
	}
	nodes := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if excludedPath(rel, effective) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		nodes++
		if nodes > effective.MaxFiles*4 {
			return fmt.Errorf("snapshot directory/file entry limit exceeded")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("snapshot refuses symlink %s", rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			s.Directories[rel] = info.Mode().Perm()
			s.Manifest.Entries = append(s.Manifest.Entries, InputEntry{Path: rel, Kind: "directory", Mode: uint32(info.Mode().Perm())})
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("not a regular file: %s", rel)
		}
		if len(s.Files) >= effective.MaxFiles {
			return fmt.Errorf("snapshot file count exceeds %d", effective.MaxFiles)
		}
		remaining := effective.MaxTotalBytes - s.Bytes
		limit := effective.MaxFileBytes
		if remaining < limit {
			limit = remaining
		}
		if info.Size() > limit {
			return fmt.Errorf("snapshot input limit exceeded by %s (file limit %d, remaining total %d bytes); adjust reviewed inputs policy", rel, effective.MaxFileBytes, remaining)
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		current, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if !os.SameFile(info, current) || !current.Mode().IsRegular() {
			f.Close()
			return fmt.Errorf("snapshot input changed while opening: %s", rel)
		}
		data, err := readInput(ctx, f, limit)
		after, statErr := f.Stat()
		closeErr := f.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		if statErr != nil {
			return statErr
		}
		if closeErr != nil {
			return closeErr
		}
		if after.Size() != int64(len(data)) || !after.ModTime().Equal(current.ModTime()) {
			return fmt.Errorf("snapshot input changed while reading: %s", rel)
		}
		latest, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if latest.Mode()&os.ModeSymlink != 0 || !os.SameFile(current, latest) {
			return fmt.Errorf("snapshot input path changed: %s", rel)
		}
		s.Files[rel] = data
		s.Modes[rel] = info.Mode().Perm()
		s.Bytes += int64(len(data))
		s.Manifest.Entries = append(s.Manifest.Entries, InputEntry{Path: rel, Kind: "file", Bytes: int64(len(data)), Mode: uint32(info.Mode().Perm()), SHA256: hashBytes(data)})
		return nil
	})
	if err != nil {
		return s, err
	}
	sort.Slice(s.Manifest.Entries, func(i, j int) bool { return s.Manifest.Entries[i].Path < s.Manifest.Entries[j].Path })
	encoded, err := json.Marshal(s.Manifest)
	if err != nil {
		return s, err
	}
	s.Hash = hashBytes(encoded)
	return s, nil
}
func readInput(ctx context.Context, reader io.Reader, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("snapshot total input limit exceeded")
	}
	var data []byte
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, err := reader.Read(buffer)
		if n > 0 {
			if int64(len(data))+int64(n) > limit {
				return nil, fmt.Errorf("snapshot input exceeds its byte budget")
			}
			data = append(data, buffer[:n]...)
		}
		if err == io.EOF {
			return data, nil
		}
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
	}
}
func (s Snapshot) Materialize(root string) error {
	return s.MaterializeContext(context.Background(), root)
}
func (s Snapshot) MaterializeContext(ctx context.Context, root string) error {
	// Create directories writable while populating them; restore exact input
	// permissions only after files are written, including empty directories.
	var dirs []string
	for path := range s.Directories {
		dirs = append(dirs, path)
	}
	sort.Strings(dirs)
	for _, path := range dirs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(path)), 0755); err != nil {
			return err
		}
	}
	for path, data := range s.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		dest := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, data, s.Modes[path]); err != nil {
			return err
		}
		if err := os.Chmod(dest, s.Modes[path]); err != nil {
			return err
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(dirs[i])), s.Directories[dirs[i]]); err != nil {
			return err
		}
	}
	return nil
}
func (s Snapshot) Record() InputRecord {
	return InputRecord{Algorithm: s.Manifest.Algorithm, Files: len(s.Files), Directories: len(s.Directories), Bytes: s.Bytes, Policy: s.Policy, Manifest: "inputs.json"}
}

// Cleanup changes permissions only in a disposable clone owned by this run.
func removeScratch(root string) error {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0700)
		}
		return nil
	})
	return os.RemoveAll(root)
}

// System temporary roots may contain aliases such as macOS /var -> /private/var.
// Resolve the operator-selected base once; continue rejecting links within inputs.
func createScratch(prefix string) (string, error) {
	base, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", err
	}
	return os.MkdirTemp(base, prefix)
}
