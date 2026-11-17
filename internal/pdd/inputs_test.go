package pdd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSnapshotBudgetsAndExplicitAssetExclusion(t *testing.T) {
	root, p := fixture(t)
	putFile(t, root, "large-assets/video.dat", strings.Repeat("x", 4096))
	policy := InputPolicy{MaxFileBytes: 1024, MaxTotalBytes: 8192, MaxFiles: 20}
	if _, err := CaptureContext(context.Background(), root, &policy); err == nil {
		t.Fatal("oversized asset bypassed input limits")
	}
	policy.Exclude = []string{"large-assets"}
	p.Inputs = &policy
	if err := p.Validate(root); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureContext(context.Background(), root, p.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.Files["large-assets/video.dat"]; ok {
		t.Fatal("excluded bytes were captured")
	}
	initial := snapshot.Hash
	putFile(t, root, "large-assets/video.dat", "changed excluded asset")
	again, err := CaptureContext(context.Background(), root, p.Inputs)
	if err != nil || again.Hash != initial {
		t.Fatal("excluded data changed the selected input digest")
	}
	if snapshot.Bytes > policy.MaxTotalBytes {
		t.Fatal("total byte budget exceeded")
	}
	// Byte and membership changes in included source still invalidate evidence.
	putFile(t, root, "extra.txt", "new input")
	changed, err := CaptureContext(context.Background(), root, p.Inputs)
	if err != nil || changed.Hash == initial {
		t.Fatal("new included input was not fingerprinted")
	}
}
func TestTotalAndFileCountLimits(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "one", "1234")
	putFile(t, root, "two", "5678")
	if _, err := CaptureContext(context.Background(), root, &InputPolicy{MaxFileBytes: 5, MaxTotalBytes: 7, MaxFiles: 10}); err == nil {
		t.Fatal("total limit ignored")
	}
	if _, err := CaptureContext(context.Background(), root, &InputPolicy{MaxFileBytes: 5, MaxTotalBytes: 20, MaxFiles: 1}); err == nil {
		t.Fatal("file count limit ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CaptureContext(ctx, root, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("capture ignored cancellation: %v", err)
	}
}
func TestSnapshotPreservesEmptyDirectoriesAndModes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "fixtures/empty"), 0750); err != nil {
		t.Fatal(err)
	}
	putFile(t, root, "script.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(root, "script.sh"), 0755); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(root)
	if err != nil {
		t.Fatal(err)
	}
	copy := t.TempDir()
	if err = snapshot.Materialize(copy); err != nil {
		t.Fatal(err)
	}
	again, err := Capture(copy)
	if err != nil || again.Hash != snapshot.Hash {
		t.Fatalf("materialized inputs differ: %v", err)
	}
	if info, err := os.Stat(filepath.Join(copy, "fixtures/empty")); err != nil || !info.IsDir() {
		t.Fatal("empty test fixture directory was lost")
	}
}
func TestExclusionsCannotOmitGoClosureOrEmbeddedInputs(t *testing.T) {
	root, p := packagesFixture(t)
	for _, path := range []string{"policy", "policy/types.go", "policy/policy.go", "policy/pointer.go", "handler/handler_test.go", "go.mod"} {
		p.Inputs = &InputPolicy{Exclude: []string{path}}
		if p.Validate(root) == nil {
			t.Fatalf("required input %s could be excluded", path)
		}
	}
	// The dependency is reached only through the empirical test package.
	root, p = fixture(t)
	p.Requirements = p.Requirements[1:]
	p.Checks = p.Checks[1:]
	p.Mutations = nil
	putFile(t, root, "core_test.go", "package fixture\nimport (\"testing\";\"fixture/helper\")\nfunc TestBoundary(t *testing.T){if !helper.Yes(){t.Fatal(\"no\")}}\n")
	putFile(t, root, "helper/helper.go", "package helper\nfunc Yes() bool {return true}\n")
	p.Inputs = &InputPolicy{Exclude: []string{"helper"}}
	if p.Validate(root) == nil {
		t.Fatal("empirical local import source could be omitted")
	}
	putFile(t, root, "core_test.go", "package fixture\nimport (\"testing\";_ \"embed\")\n//go:embed fixtures/*\nvar value string\nfunc TestBoundary(t *testing.T){}\n")
	putFile(t, root, "fixtures/message.txt", "important embedded input")
	p.Inputs = &InputPolicy{Exclude: []string{"fixtures"}}
	if p.Validate(root) == nil {
		t.Fatal("embedded file could be excluded")
	}
	p.Inputs = &InputPolicy{Exclude: []string{"fixtures/message.txt"}}
	if p.Validate(root) == nil {
		t.Fatal("one embedded match could be excluded")
	}
	putFile(t, root, "testdata/state.json", "{}")
	p.Inputs = &InputPolicy{Exclude: []string{"testdata/state.json"}}
	if p.Validate(root) == nil {
		t.Fatal("testdata could be excluded")
	}
}
func TestInputPolicyIsReviewedAndManifestIsChecked(t *testing.T) {
	root, p := fixture(t)
	p.Inputs = &InputPolicy{Exclude: []string{"output"}}
	if err := WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	approveFixture(t, root, p)
	receipt, dir, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil || !receipt.Success {
		t.Fatalf("prove: %v %v", err, receipt.Gaps)
	}
	if receipt.Inputs == nil || receipt.Inputs.Files == 0 || receipt.Inputs.ManifestSHA256 == "" {
		t.Fatal("receipt did not record bounded input provenance")
	}
	if _, _, err := Status(root, p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inputs.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Status(root, p); err == nil {
		t.Fatal("altered input manifest accepted")
	}
	p.Inputs.Exclude = []string{"different-output"}
	if err := WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	if CheckApproval(root, p) == nil {
		t.Fatal("input scope changed without renewed review")
	}
}
func TestInvalidInputPoliciesRejected(t *testing.T) {
	for _, policy := range []InputPolicy{{Exclude: []string{".."}}, {Exclude: []string{"../secret"}}, {Exclude: []string{"cache/"}}, {Exclude: []string{"*.go"}}, {Exclude: []string{"cache", "cache"}}, {MaxFiles: -1}, {MaxTotalBytes: 1}, {MaxFileBytes: 257 << 20}} {
		if _, err := effectivePolicy(&policy); err == nil {
			t.Fatalf("invalid policy accepted: %+v", policy)
		}
	}
	patterns, err := embedPatterns("assets/* \"with space/file.txt\" `raw/path`")
	if err != nil || strings.Join(patterns, ",") != "assets/*,with space/file.txt,raw/path" {
		t.Fatalf("embed patterns: %v %v", patterns, err)
	}
}

func TestEmpiricalVendoredAndReplacedInputsProtected(t *testing.T) {
	root, p := fixture(t)
	p.Requirements = p.Requirements[1:]
	p.Checks = p.Checks[1:]
	p.Mutations = nil
	putFile(t, root, "vendor/modules.txt", "# example.org/helper v1.0.0\n")
	p.Inputs = &InputPolicy{Exclude: []string{"vendor/example.org/helper"}}
	if p.Validate(root) == nil {
		t.Fatal("vendored implementation could be excluded")
	}
	p.Inputs = &InputPolicy{Exclude: []string{"local-helper/lib.go"}}
	putFile(t, root, "go.mod", "module fixture\ngo 1.20\nreplace example.org/helper => ./local-helper\n")
	putFile(t, root, "local-helper/lib.go", "package helper\n")
	if p.Validate(root) == nil {
		t.Fatal("local replacement could be excluded")
	}
	putFile(t, root, "go.mod", "module fixture\ngo 1.20\nreplace example.org/helper => ..\n")
	p.Inputs = nil
	if p.Validate(root) == nil {
		t.Fatal("external replacement escaped source snapshot")
	}
	paths, err := localReplacements(root, "replace (\nexample.org/helper => .\n)\n")
	if err != nil || len(paths) != 1 || !containsPath(paths[0].path, "anything/file.go") {
		t.Fatalf("root replacement not protected: %v %v", paths, err)
	}
}
func TestManifestSummaryTamperingRejected(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	receipt, dir, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil || !receipt.Success {
		t.Fatalf("prove: %v %v", err, receipt.Gaps)
	}
	receipt.Inputs.Bytes++
	if err = WriteJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Status(root, p); err == nil {
		t.Fatal("altered input summary accepted")
	}
}

func TestReceiptCannotDropProvenanceOrMislabelEvidence(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	original, dir, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil || !original.Success {
		t.Fatalf("prove: %v", err)
	}
	for _, change := range []func(*Receipt){
		func(r *Receipt) { r.Inputs = nil },
		func(r *Receipt) { r.Trust = nil },
		func(r *Receipt) { r.NonGoals = []string{"no limitations"} },
		func(r *Receipt) { r.Evidence[0].Kind = "empirical" },
		func(r *Receipt) { r.Evidence[0].Execution.ExitCode = 1 },
		func(r *Receipt) { r.Mutations[0].Execution.Status = "timeout" },
		func(r *Receipt) { r.Requirements[0].Check = "other" },
	} {
		// Decode afresh so slice mutations cannot affect the original fixture.
		if err = WriteJSON(filepath.Join(dir, "receipt.json"), original); err != nil {
			t.Fatal(err)
		}
		var altered Receipt
		if err = ReadJSON(filepath.Join(dir, "receipt.json"), &altered); err != nil {
			t.Fatal(err)
		}
		change(&altered)
		if err = WriteJSON(filepath.Join(dir, "receipt.json"), altered); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Status(root, p); err == nil {
			t.Fatal("contradictory receipt accepted")
		}
	}
}
