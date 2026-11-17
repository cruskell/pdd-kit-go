package pdd

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCancellationTerminatesChildProcessTree(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Execution, 1)
	go func() {
		done <- execute(ctx, root, []string{"sh", "-c", "sleep 60 & echo $! > \"$1\"; wait", "sh", pidFile}, 10*time.Second)
	}()
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("child process never started")
	}
	cancel()
	select {
	case result := <-done:
		if result.Status != "canceled" {
			t.Fatalf("cancel result: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not return promptly")
	}
	// A killed child may briefly be a zombie awaiting the host init's reaper;
	// it is no longer running and cannot retain the command's resources.
	alive := true
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			alive = false
			break
		}
		if b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
			tail := string(b[strings.LastIndex(string(b), ")")+1:])
			if strings.HasPrefix(strings.TrimSpace(tail), "Z ") {
				alive = false
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if alive {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("child survived cancellation")
	}
}
func TestDeadlineIsReportedAsTimeout(t *testing.T) {
	result := execute(context.Background(), t.TempDir(), []string{"sh", "-c", "sleep 60"}, 100*time.Millisecond)
	if result.Status != "timeout" {
		t.Fatalf("deadline result: %+v", result)
	}
}

func TestSpecialFilesCannotBlockSourceValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "blocked.go")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSource(root, "blocked.go"); err == nil {
		t.Fatal("FIFO accepted as source")
	}
	if _, err := Capture(root); err == nil {
		t.Fatal("FIFO accepted in snapshot")
	}
}
