package pdd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGobraHTTPIntegration(t *testing.T) {
	if os.Getenv("PDD_INTEGRATION") != "1" {
		t.Skip("set PDD_INTEGRATION=1 for real HTTP application policy proof")
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
	if err = ReadJSON(filepath.Join(root, "examples/httpwallet/plan.json"), &p); err != nil {
		t.Fatal(err)
	}
	p, err = p.Named("httpwallet")
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteJSON(filepath.Join(root, p.Paths().File), p); err != nil {
		t.Fatal(err)
	}
	var trust []string
	for _, item := range p.Trust {
		trust = append(trust, item.ID)
	}
	if _, err = Approve(root, p, "integration-demo", trust); err != nil {
		t.Fatal(err)
	}
	receipt, _, err := Prove(context.Background(), root, p, Options{Runner: ToolRunner{Timeout: 2 * time.Minute}, Version: "integration"})
	if err != nil || !receipt.Success {
		for _, evidence := range receipt.Evidence {
			t.Logf("%s: %s", evidence.Check, evidence.Execution.Output)
		}
		for _, mutation := range receipt.Mutations {
			t.Logf("%s: %s", mutation.ID, mutation.Execution.Output)
		}
		t.Fatalf("HTTP policy proof: %v %v", err, receipt.Gaps)
	}
	if len(receipt.Mutations) != 3 {
		t.Fatal("shipped policy lost a negative example")
	}
	if _, _, err := Status(root, p); err != nil {
		t.Fatal(err)
	}
}
