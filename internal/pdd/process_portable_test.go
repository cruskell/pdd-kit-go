package pdd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Invoked as a subprocess by the crash-recovery test, never a normal suite run.
func TestLockHolderProcess(t *testing.T) {
	root := os.Getenv("PDD_LOCK_HELPER_ROOT")
	if root == "" {
		return
	}
	unlock, err := lockProject(root)
	if err != nil {
		os.Exit(3)
	}
	defer unlock()
	if err = os.WriteFile(filepath.Join(root, ".pdd/holder-ready"), []byte("ready"), 0600); err != nil {
		os.Exit(4)
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestProjectLockRecoversAfterProcessCrash(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHolderProcess$")
	cmd.Env = append(os.Environ(), "PDD_LOCK_HELPER_ROOT="+root)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if _, err := os.Stat(filepath.Join(root, ".pdd/holder-ready")); err == nil {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("lock holder did not become ready")
	}
	if unlock, err := lockProject(root); err == nil {
		unlock()
		t.Fatal("second process acquired the same project lock")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	unlock, err := lockProject(root)
	if err != nil {
		t.Fatalf("dead process left a stale lock: %v", err)
	}
	unlock()
}

// The helper runs in a separate OS process; it uses no shell or mock runner.
func TestPortableProcessHelper(t *testing.T) {
	role := os.Getenv("PDD_PROCESS_ROLE")
	if role == "" {
		return
	}
	if role == "parent" {
		child := exec.Command(os.Args[0], "-test.run=^TestPortableProcessHelper$")
		child.Env = append(os.Environ(), "PDD_PROCESS_ROLE=child")
		// Pipes are deliberately inherited to exercise cleanup of descendants.
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if child.Start() != nil {
			os.Exit(3)
		}
		_ = child.Wait()
		return
	}
	if role == "child" {
		if os.WriteFile(os.Getenv("PDD_PROCESS_PID"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(4)
		}
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestPortableProcessTreeCancellation(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("PDD_PROCESS_ROLE", "parent")
	t.Setenv("PDD_PROCESS_PID", pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Execution, 1)
	go func() {
		done <- execute(ctx, t.TempDir(), []string{os.Args[0], "-test.run=^TestPortableProcessHelper$"}, 20*time.Second)
	}()
	pid := 0
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if b, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.Atoi(string(b))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("child never became ready")
	}
	cancel()
	select {
	case result := <-done:
		if result.Status != "canceled" {
			t.Fatalf("cancel: %+v", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("process cancellation hung")
	}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if !processRunning(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	child, err := os.FindProcess(pid)
	if err == nil {
		_ = child.Kill()
	}
	t.Fatal("descendant survived process-tree cancellation")
}
func TestPortableProcessTimeout(t *testing.T) {
	t.Setenv("PDD_PROCESS_ROLE", "idle")
	result := execute(context.Background(), t.TempDir(), []string{os.Args[0], "-test.run=^TestPortableProcessHelper$"}, 200*time.Millisecond)
	if result.Status != "timeout" {
		t.Fatalf("timeout: %+v", result)
	}
}
