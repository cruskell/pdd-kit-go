package pdd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortableApprovalPreservesReviewAndRejectsChangedIntent(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	exported, err := ExportApproval(root, p, ".pdd/reviews/review.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ExportApproval(root, p, ".pdd/reviews/review.json"); err != nil {
		t.Fatal("identical export rejected", err)
	}
	if err = os.Remove(filepath.Join(root, p.stateFile("approval.json"))); err != nil {
		t.Fatal(err)
	}
	if CheckApproval(root, p) == nil {
		t.Fatal("missing approval accepted")
	}
	imported, err := ImportApproval(root, p, ".pdd/reviews/review.json")
	if err != nil || !sameApproval(imported, exported) {
		t.Fatalf("import changed assertion: %v", err)
	}
	if err = CheckApproval(root, p); err != nil {
		t.Fatal(err)
	}
	if _, err = ExportApproval(root, p, "core.go"); err == nil {
		t.Fatal("export overwrote source")
	}
	p.Intent += " changed"
	if err = WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	if _, err = ImportApproval(root, p, ".pdd/reviews/review.json"); err == nil {
		t.Fatal("old review approved changed intent")
	}
}
func TestPortableApprovalRejectsIncompleteOrAlteredRecords(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	var original Approval
	if err := ReadJSON(filepath.Join(root, ApprovalPath), &original); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Approval){
		func(a *Approval) { a.Review = nil },
		func(a *Approval) { a.PlanID = "another-plan" },
		func(a *Approval) { a.Digest = "fake" },
		func(a *Approval) { a.AcceptedTrust = nil },
		func(a *Approval) { a.Reviewer = "" },
		func(a *Approval) { review := *a.Review; review.Plan.Intent += " changed"; a.Review = &review },
	} {
		modified := original
		change(&modified)
		if err := WriteJSON(filepath.Join(root, ".pdd/reviews/invalid.json"), modified); err != nil {
			t.Fatal(err)
		}
		if _, err := ImportApproval(root, p, ".pdd/reviews/invalid.json"); err == nil {
			t.Fatal("invalid artifact accepted")
		}
		var active Approval
		if err := ReadJSON(filepath.Join(root, ApprovalPath), &active); err != nil || !sameApproval(active, original) {
			t.Fatal("failed import changed active approval", err)
		}
	}
	unlock, err := lockProject(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if _, err = ImportApproval(root, p, ".pdd/reviews/invalid.json"); err == nil {
		t.Fatal("import raced active run")
	}
	if _, err = ExportApproval(root, p, ".pdd/reviews/locked.json"); err == nil {
		t.Fatal("export raced active run")
	}
}
