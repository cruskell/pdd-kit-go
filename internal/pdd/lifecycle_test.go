package pdd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type gatedRunner struct {
	entered  chan struct{}
	release  chan struct{}
	delegate fakeRunner
}

func (r *gatedRunner) Run(ctx context.Context, root string, c Check) Execution {
	if r.entered != nil {
		close(r.entered)
		r.entered = nil
		select {
		case <-r.release:
		case <-ctx.Done():
			return Execution{Status: "canceled", ExitCode: -1, Detail: ctx.Err().Error()}
		}
	}
	return r.delegate.Run(ctx, root, c)
}
func TestConcurrentRunAndApprovalAreRejected(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	if _, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}}); err != nil {
		t.Fatal(err)
	}
	runner := &gatedRunner{entered: make(chan struct{}), release: make(chan struct{})}
	entered := runner.entered
	finished := make(chan error, 1)
	go func() {
		r, _, err := Prove(context.Background(), root, p, Options{Runner: runner})
		if err == nil && !r.Success {
			err = os.ErrInvalid
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(runner.release)
		t.Fatal("verification did not reach runner")
	}
	released := false
	defer func() {
		if !released {
			close(runner.release)
		}
	}()
	if _, _, err := Status(root, p); err == nil {
		t.Fatal("old green receipt concealed a newer incomplete run")
	}
	if _, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}}); err == nil {
		t.Fatal("concurrent proof was accepted")
	}
	if _, err := Approve(root, p, "another-reviewer", []string{"identity"}); err == nil {
		t.Fatal("approval changed during active verification")
	}
	close(runner.release)
	released = true
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("verification did not finish")
	}
	if _, _, err := Status(root, p); err != nil {
		t.Fatal(err)
	}
}

type panicRunner struct{}

func (panicRunner) Run(context.Context, string, Check) Execution {
	panic("simulated abrupt runner failure")
}
func TestInterruptedNewestRunDoesNotReuseOlderSuccess(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	if _, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}}); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("runner did not panic")
			}
		}()
		_, _, _ = Prove(context.Background(), root, p, Options{Runner: panicRunner{}})
	}()
	state, err := readRunState(root)
	if err != nil || state.State != "interrupted" {
		t.Fatalf("interrupted state: %+v %v", state, err)
	}
	if _, _, err = Status(root, p); err == nil {
		t.Fatal("interrupted newest run returned old success")
	}
	// Kernel-backed lock is released by defers; a subsequent run can recover.
	receipt, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil || !receipt.Success {
		t.Fatalf("recovery: %v %v", err, receipt.Gaps)
	}
}
func TestCanceledBeforeCaptureDoesNotReuseOlderSuccess(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	if _, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeRunner{}
	receipt, _, err := Prove(ctx, root, p, Options{Runner: runner})
	if !errors.Is(err, context.Canceled) || receipt.Success || runner.calls != 0 {
		t.Fatalf("pre-capture cancellation: %v %+v", err, receipt)
	}
	if _, _, err = Status(root, p); err == nil {
		t.Fatal("pre-capture interruption reused old success")
	}
}
func TestCanceledDuringRunWritesNegativeReceipt(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	ctx, cancel := context.WithCancel(context.Background())
	runner := &cancelRunner{cancel: cancel}
	receipt, _, err := Prove(ctx, root, p, Options{Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Success || runner.calls != 1 {
		t.Fatal("canceled run executed later checks")
	}
	for _, verdict := range receipt.Requirements {
		if verdict.Status != "unproven" {
			t.Fatal("canceled run retained a positive verdict")
		}
	}
	if _, _, err = Status(root, p); err != nil {
		t.Fatal(err)
	}
}

type cancelRunner struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancelRunner) Run(context.Context, string, Check) Execution {
	r.calls++
	r.cancel()
	return Execution{Status: "canceled", ExitCode: -1, Detail: "canceled by test"}
}

func TestLegacyNewestIncompleteReceiptFailsClosed(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	_, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, runStatePath)); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root, ".pdd/receipts/99991231T235959.999999999Z"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Status(root, p); err == nil {
		t.Fatal("legacy scan skipped newest unfinished receipt")
	}
}
func TestAmbiguousJSONKeysRejected(t *testing.T) {
	for _, body := range []string{`{"schema":1,"schema":2}`, `{"schema":1,"SCHEMA":2}`, `{"schema":1,"ſchema":2}`, `{"checks":[{"id":"one","id":"two"}]}`} {
		path := filepath.Join(t.TempDir(), "plan.json")
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		var value interface{}
		if err := ReadJSON(path, &value); err == nil || !strings.Contains(err.Error(), "duplicate JSON key") {
			t.Fatalf("ambiguous JSON accepted: %s %v", body, err)
		}
	}
}

func TestMutationDiagnosticBindsFileFunctionAndClause(t *testing.T) {
	source := []byte("package sample\n// @ ensures result\nfunc First() (result bool) { return true }\n// @ ensures result\nfunc Second() (result bool) { return true }\n")
	target := Requirement{File: "core.go", Function: "First", Ensures: "result"}
	cases := []struct {
		name, output string
		want         bool
	}{
		{"target", "Error at: </workspace/core.go:2:9> Postcondition might not hold.\nAssertion result might not hold.", true},
		{"another method", "Error at: </workspace/core.go:4:9> Postcondition might not hold.\nAssertion result might not hold.", false},
		{"another file", "Error at: </workspace/other.go:2:9> Postcondition might not hold.\nAssertion result might not hold.", false},
		{"no source position", "Postcondition might not hold. Assertion result might not hold.", false},
		{"different clause", "Error at: </workspace/core.go:2:9> Postcondition might not hold.\nAssertion !result might not hold.", false},
		{"bad precondition", "Error at: </workspace/core.go:2:9> Precondition might not hold.\nAssertion result might not hold.", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			execution := Execution{Status: "failed", ExitCode: 1, Output: test.output}
			if got := RejectedMutation(execution, target, source); got != test.want {
				t.Fatalf("targeted rejection=%v, want %v", got, test.want)
			}
		})
	}
}
