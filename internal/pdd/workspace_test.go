package pdd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func saveNamed(t *testing.T, root string, p Plan, name string) Plan {
	t.Helper()
	named, err := p.Named(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteJSON(filepath.Join(root, named.Paths().File), named); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadNamed(root, name)
	if err != nil {
		t.Fatal(err)
	}
	return loaded
}
func TestNamedPlansKeepApprovalsAndReceiptsIndependent(t *testing.T) {
	root, defaultPlan := fixture(t)
	named := saveNamed(t, root, defaultPlan, "payments")
	defaultDigest, err := Digest(root, defaultPlan)
	if err != nil {
		t.Fatal(err)
	}
	namedDigest, err := Digest(root, named)
	if err != nil {
		t.Fatal(err)
	}
	if defaultDigest == namedDigest {
		t.Fatal("identical plan data shared its approval identity across names")
	}
	for _, p := range []Plan{defaultPlan, named} {
		approveFixture(t, root, p)
		receipt, dir, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
		if err != nil || !receipt.Success {
			t.Fatalf("%s: %v %v", p.ID(), err, receipt.Gaps)
		}
		if !strings.Contains(dir, filepath.FromSlash(p.Paths().State)) {
			t.Fatal("receipt escaped selected state namespace")
		}
	}
	for _, p := range []Plan{defaultPlan, named} {
		if _, _, err := Status(root, p); err != nil {
			t.Fatalf("another plan invalidated %s: %v", p.ID(), err)
		}
	}
	// A copied default approval must not authorize a named plan.
	original, err := os.ReadFile(filepath.Join(root, named.stateFile("approval.json")))
	if err != nil {
		t.Fatal(err)
	}
	other, err := os.ReadFile(filepath.Join(root, ApprovalPath))
	if err != nil {
		t.Fatal(err)
	}
	if err = AtomicWrite(filepath.Join(root, named.stateFile("approval.json")), other); err != nil {
		t.Fatal(err)
	}
	if CheckApproval(root, named) == nil {
		t.Fatal("approval copied across plan namespaces was accepted")
	}
	if err = AtomicWrite(filepath.Join(root, named.stateFile("approval.json")), original); err != nil {
		t.Fatal(err)
	}
	// Copying or relabeling a receipt is also rejected, independently of times.
	receipt, dir, err := Status(root, named)
	if err != nil {
		t.Fatal(err)
	}
	receipt.PlanID = "default"
	if err = WriteJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Status(root, named); err == nil {
		t.Fatal("receipt identity mismatch accepted")
	}
	if _, _, err = Status(root, defaultPlan); err != nil {
		t.Fatal("named receipt corruption changed default status")
	}
}
func TestPlanNameValidationAndNoDefaultFallback(t *testing.T) {
	root, _ := fixture(t)
	for _, name := range []string{"../other", "Name", "a/b", "a\\b", "..", "/tmp/plan", "a b"} {
		if _, err := PathsFor(name); err == nil {
			t.Fatalf("invalid name %q accepted", name)
		}
	}
	if _, err := LoadNamed(root, "missing"); err == nil {
		t.Fatal("missing named plan fell back to the default")
	}
	defaultPaths, _ := PathsFor("")
	explicitPaths, _ := PathsFor("default")
	if defaultPaths != explicitPaths {
		t.Fatal("default aliases use different state")
	}
}
func TestStoredReviewShowsIntentAndContractChanges(t *testing.T) {
	root, p := fixture(t)
	// Include a pure definition: changing its meaning must appear in the review.
	source := coreSource + "\n// @ decreases\n// @ pure\nfunc enabled(value bool) bool { return value }\n"
	putFile(t, root, "core.go", source)
	approveFixture(t, root, p)
	report, err := ReportReview(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ChangesAvailable || len(report.Changes) != 0 || report.Approval.State != "current" {
		t.Fatalf("initial review: %+v", report)
	}
	p.Intent = "Only the newly reviewed intent may be implemented."
	if err = WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	putFile(t, root, "core.go", strings.Replace(source, "return value", "return true", 1))
	report, err = ReportReview(root, p)
	if err != nil {
		t.Fatal(err)
	}
	foundIntent, foundDefinition := false, false
	for _, change := range report.Changes {
		foundIntent = foundIntent || change.Path == "plan.intent"
		foundDefinition = foundDefinition || change.Path == "sources.core.go.definitions"
	}
	if !foundIntent || !foundDefinition || report.Approval.State != "stale" {
		t.Fatalf("missing semantic review changes: %+v", report.Changes)
	}
}
func TestExpectedDigestDoesNotApproveUnreviewedChanges(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	before, err := os.ReadFile(filepath.Join(root, ApprovalPath))
	if err != nil {
		t.Fatal(err)
	}
	oldDigest, err := Digest(root, p)
	if err != nil {
		t.Fatal(err)
	}
	p.Requirements[0].Text = "The revised requirement must receive a new review."
	if err = WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	if _, err = ApproveExpected(root, p, "reviewer", []string{"identity"}, oldDigest); err == nil {
		t.Fatal("stale expected digest was approved")
	}
	after, err := os.ReadFile(filepath.Join(root, ApprovalPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed compare-and-approve overwrote the previous approval")
	}
	current, err := Digest(root, p)
	if err != nil {
		t.Fatal(err)
	}
	approval, err := ApproveExpected(root, p, "reviewer", []string{"identity"}, current)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Review == nil || approval.Digest != current {
		t.Fatal("approved review content was not retained")
	}
	if err = CheckApproval(root, p); err != nil {
		t.Fatal(err)
	}
}
func TestApprovalRejectsChangedOnDiskPlanAndDamagedReview(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	changed := p
	changed.Intent = "Changed on disk"
	if err := WriteJSON(filepath.Join(root, PlanPath), changed); err != nil {
		t.Fatal(err)
	}
	if _, err := Approve(root, p, "reviewer", []string{"identity"}); err == nil {
		t.Fatal("cached plan approved different on-disk intent")
	}
	if err := WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	var approval Approval
	if err := ReadJSON(filepath.Join(root, ApprovalPath), &approval); err != nil {
		t.Fatal(err)
	}
	approval.Review.Plan.Intent = "Tampered saved review"
	if err := WriteJSON(filepath.Join(root, ApprovalPath), approval); err != nil {
		t.Fatal(err)
	}
	if CheckApproval(root, p) == nil {
		t.Fatal("damaged saved review accepted")
	}
	report, err := ReportReview(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if report.Approval.State != "invalid" || report.ChangesAvailable {
		t.Fatal("corrupt historical content was treated as a valid diff baseline")
	}
}
func TestLegacyDefaultApprovalRemainsReadable(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	var approval Approval
	if err := ReadJSON(filepath.Join(root, ApprovalPath), &approval); err != nil {
		t.Fatal(err)
	}
	approval.PlanID = ""
	approval.Review = nil
	if err := WriteJSON(filepath.Join(root, ApprovalPath), approval); err != nil {
		t.Fatal(err)
	}
	if err := CheckApproval(root, p); err != nil {
		t.Fatal(err)
	}
	report, err := ReportReview(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if report.ChangesAvailable || report.Approval.State != "current" {
		t.Fatal("legacy approval was misrepresented")
	}
}

func TestApprovalBindsPreconditionsToTheirFunction(t *testing.T) {
	root, p := fixture(t)
	helper := "func helper(present bool) bool { return present }\n"
	before := "package fixture\n// @ requires present\n" + helper + strings.TrimPrefix(coreSource, "package fixture\n")
	putFile(t, root, "core.go", before)
	approveFixture(t, root, p)
	// Body and line-count edits must not spuriously change the contract approval.
	putFile(t, root, "core.go", strings.Replace(before, "{ return present }", "{\n if present {return !present}\n return present\n}", 1))
	if err := CheckApproval(root, p); err != nil {
		t.Fatalf("ordinary helper body edit invalidated review: %v", err)
	}
	// Annotation order and Go declarations remain identical, but the precondition
	// now excludes inputs of Decide that were previously required to be checked.
	after := "package fixture\n" + helper + "// @ requires present\n" + strings.TrimPrefix(coreSource, "package fixture\n")
	putFile(t, root, "core.go", after)
	if CheckApproval(root, p) == nil {
		t.Fatal("moving requires to the verified function weakened its approved contract")
	}
	report, err := ReportReview(root, p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range report.Changes {
		found = found || change.Path == "sources.core.go.bindings"
	}
	if !found {
		t.Fatal("precondition ownership change was absent from review diff")
	}
}
func TestJSONWriteLimitKeepsPreviousArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	if err := WriteJSON(path, "previous"); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(path, strings.Repeat("x", 8<<20)); err == nil {
		t.Fatal("wrote an artifact too large to read back")
	}
	var previous string
	if err := ReadJSON(path, &previous); err != nil || previous != "previous" {
		t.Fatalf("oversized write replaced the prior artifact: %q %v", previous, err)
	}
}
