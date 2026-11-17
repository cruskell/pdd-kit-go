package pdd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGobraSignedValidationIntegration(t *testing.T) {
	if os.Getenv("PDD_INTEGRATION") != "1" {
		t.Skip("set PDD_INTEGRATION=1 for real permission-aware slice proof and signed policy gate")
	}
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(repository)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = snapshot.Materialize(root); err != nil {
		t.Fatal(err)
	}
	var p Plan
	if err = ReadJSON(filepath.Join(root, "examples/validation/plan.json"), &p); err != nil {
		t.Fatal(err)
	}
	p, err = p.Named("validation")
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteJSON(filepath.Join(root, p.Paths().File), p); err != nil {
		t.Fatal(err)
	}
	keys := t.TempDir()
	secret := filepath.Join(keys, "private.pem")
	identity, err := GenerateSigningKey(secret, filepath.Join(keys, "public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(keys, "policy.json")
	if err = WriteJSON(path, ReviewPolicy{Schema: 1, MaxAgeHours: 1, Reviewers: []AuthorizedReviewer{{Name: "integration-demo", PublicKey: identity.PublicKey, Plans: []string{"validation"}}}}); err != nil {
		t.Fatal(err)
	}
	digest, err := Digest(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ApproveSignedWithPolicy(root, p, "integration-demo", nil, digest, secret, path); err != nil {
		t.Fatal(err)
	}
	receipt, _, err := Prove(context.Background(), root, p, Options{PolicyPath: path, Version: "integration", Runner: ToolRunner{Timeout: 2 * time.Minute}})
	if err != nil || !receipt.Success {
		for _, e := range receipt.Evidence {
			t.Logf("%s: %s", e.Check, e.Execution.Output)
		}
		for _, m := range receipt.Mutations {
			t.Logf("%s: %s", m.ID, m.Execution.Output)
		}
		t.Fatalf("signed slice proof: %v %v", err, receipt.Gaps)
	}
	if _, _, err = StatusWithPolicy(root, p, path); err != nil {
		t.Fatal(err)
	}
}
