package pdd

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestStatusCannotAcceptEvidenceWhileProjectIsLocked(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	receipt, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil || !receipt.Success {
		t.Fatal("fixture proof failed", err)
	}
	unlock, err := lockProject(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, _, err = Status(root, p); err == nil {
		t.Fatal("status accepted an old receipt while a mutating project operation held the lock")
	}
}

func TestSignedApprovalCannotBeDowngradedByApproveOrImport(t *testing.T) {
	for _, operation := range []string{"approve", "import"} {
		t.Run(operation, func(t *testing.T) {
			root, p, _, _, signed := signingFixture(t)
			var err error
			if operation == "approve" {
				_, err = Approve(root, p, "maintainer", []string{"identity"})
			} else {
				unsigned := signed
				unsigned.Signature = nil
				path := filepath.Join(root, ".pdd/reviews/unsigned.json")
				if e := WriteJSON(path, unsigned); e != nil {
					t.Fatal(e)
				}
				_, err = ImportApproval(root, p, path)
			}
			if err == nil {
				t.Fatal("signed review silently replaced by an unsigned assertion")
			}
			var active Approval
			if e := ReadJSON(filepath.Join(root, ApprovalPath), &active); e != nil {
				t.Fatal(e)
			}
			if !sameApproval(active, signed) {
				t.Fatal("rejected downgrade modified the active signed approval")
			}
		})
	}
}

func TestStatusRechecksSuccessfulReceiptLogs(t *testing.T) {
	for _, kind := range []string{"formal-summary", "contradictory-formal-summary", "required-test", "incomplete-package", "failed-test-event", "missing-mutant-log", "unrelated-mutant-failure"} {
		t.Run(kind, func(t *testing.T) {
			root, p := fixture(t)
			approveFixture(t, root, p)
			receipt, dir, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
			if err != nil || !receipt.Success {
				t.Fatal("fixture proof failed", err)
			}
			replaceLog := func(name string, data string) string {
				t.Helper()
				if err := AtomicWrite(filepath.Join(dir, name), []byte(data)); err != nil {
					t.Fatal(err)
				}
				return hashBytes([]byte(data))
			}
			switch kind {
			case "formal-summary":
				receipt.Evidence[0].LogSHA256 = replaceLog(receipt.Evidence[0].Log, "Gobra found 0 errors.\n")
			case "contradictory-formal-summary":
				receipt.Evidence[0].LogSHA256 = replaceLog(receipt.Evidence[0].Log, "Verifying package fixture\nGobra found 0 errors.\nGobra found 1 error.\n")
			case "required-test":
				receipt.Evidence[1].LogSHA256 = replaceLog(receipt.Evidence[1].Log, "{\"Action\":\"pass\",\"Test\":\"TestUnrelated\"}\n")
			case "incomplete-package":
				receipt.Evidence[1].LogSHA256 = replaceLog(receipt.Evidence[1].Log, "{\"Action\":\"pass\",\"Package\":\"fixture\",\"Test\":\"TestBoundary\"}\n")
			case "failed-test-event":
				receipt.Evidence[1].LogSHA256 = replaceLog(receipt.Evidence[1].Log, "{\"Action\":\"pass\",\"Test\":\"TestBoundary\"}\n{\"Action\":\"fail\"}\n")
			case "missing-mutant-log":
				receipt.Mutations[0].Log = ""
				receipt.Mutations[0].LogSHA256 = ""
			case "unrelated-mutant-failure":
				receipt.Mutations[0].LogSHA256 = replaceLog(receipt.Mutations[0].Log, "syntax error\n")
			}
			if err = WriteJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
				t.Fatal(err)
			}
			if _, _, err = Status(root, p); err == nil {
				t.Fatal("success accepted without the required saved verifier/test/mutation output")
			}
		})
	}
}

type diagnosticFixtureRunner struct{ delegate fakeRunner }

func (r *diagnosticFixtureRunner) Run(ctx context.Context, root string, c Check) Execution {
	e := r.delegate.Run(ctx, root, c)
	if c.Kind == "go-test" {
		// This raw marker is JSON-escaped inside the event; it is not the stream separator.
		e.Output += "{\"Action\":\"output\",\"Output\":\"example\\n[stderr]\\ntext\"}\n"
		e.Output += "\n[stderr]\ngo: downloading example.org/diagnostic v1.0.0\n"
	}
	return e
}
func TestStatusRetainsValidGoDiagnosticEvidence(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	receipt, _, err := Prove(context.Background(), root, p, Options{Runner: &diagnosticFixtureRunner{}})
	if err != nil || !receipt.Success {
		t.Fatalf("fixture failed: %v %v", err, receipt.Gaps)
	}
	if _, _, err = Status(root, p); err != nil {
		t.Fatal("valid test JSON and separate diagnostics rejected:", err)
	}
}
func TestTruncatedPostconditionFailureIsInconclusive(t *testing.T) {
	_, p := fixture(t)
	e := Execution{Status: "failed", ExitCode: 1, Output: proofFailureOutput("core.go", []byte(coreSource), p.Requirements[0].Ensures) + "\n[pdd: output truncated]\n"}
	if RejectedMutation(e, p.Requirements[0], []byte(coreSource)) {
		t.Fatal("truncated output certified a killed mutant")
	}
}
func TestSignedApprovalCanBeRenewedWithAuthorizedSignature(t *testing.T) {
	root, p, key, policy, _ := signingFixture(t)
	digest, err := Digest(root, p)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := ApproveSignedWithPolicy(root, p, "maintainer", []string{"identity"}, digest, key, policy)
	if err != nil || renewed.Signature == nil {
		t.Fatal("authorized renewal rejected", err)
	}
	if err = CheckApproval(root, p); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledStatusNeverReturnsOldGreen(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	r, _, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil || !r.Success {
		t.Fatal("fixture failed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err = StatusContext(ctx, root, p, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled status returned %v", err)
	}
}
