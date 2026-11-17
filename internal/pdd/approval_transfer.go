package pdd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ExportApproval preserves a complete reviewed assertion without creating a new
// review or timestamp. The destination should be a version-controlled artifact.
func ExportApproval(root string, p Plan, destination string) (Approval, error) {
	unlock, err := lockProject(root)
	if err != nil {
		return Approval{}, err
	}
	defer unlock()
	if err = p.Validate(root); err != nil {
		return Approval{}, err
	}
	if err = CheckApproval(root, p); err != nil {
		return Approval{}, err
	}
	var approval Approval
	if err = ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &approval); err != nil {
		return approval, err
	}
	if approval.Review == nil {
		return approval, fmt.Errorf("portable approval requires a saved review; review and approve again before exporting")
	}
	if destination == "" {
		return approval, fmt.Errorf("--to is required")
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(root, destination)
	}
	// Never silently overwrite a source file or another review. Re-exporting
	// identical content is harmless; choose another path for a new assertion.
	if _, err = os.Lstat(destination); err == nil {
		var existing Approval
		if err = ReadJSON(destination, &existing); err != nil || !sameApproval(approval, existing) {
			return approval, fmt.Errorf("export destination already exists with different content; choose another --to path")
		}
		return approval, nil
	} else if !os.IsNotExist(err) {
		return approval, err
	}
	return approval, WriteJSON(destination, approval)
}

// ImportApproval validates and restores an existing assertion. It never approves
// changed intent, accepts new trust, or replaces its reviewer/timestamp.
func ImportApproval(root string, p Plan, source string) (Approval, error) {
	unlock, err := lockProject(root)
	if err != nil {
		return Approval{}, err
	}
	defer unlock()
	if err = p.Validate(root); err != nil {
		return Approval{}, err
	}
	if err = authoredPlanMatches(root, p); err != nil {
		return Approval{}, err
	}
	if source == "" {
		return Approval{}, fmt.Errorf("--from is required")
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(root, source)
	}
	var approval Approval
	if err = ReadJSON(source, &approval); err != nil {
		return approval, err
	}
	if approval.Review == nil {
		return approval, fmt.Errorf("portable approval requires a saved review")
	}
	if err = validateApproval(root, p, approval); err != nil {
		return approval, err
	}
	// Verify the full frozen bundle against the current review as well as its
	// digest, so an imported artifact is directly reviewable in code review.
	current, err := BuildReview(root, p)
	if err != nil {
		return approval, err
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(approval.Review)
	if string(a) != string(b) {
		return approval, fmt.Errorf("imported saved review differs from current intent and contracts")
	}
	return approval, WriteJSON(filepath.Join(root, p.stateFile("approval.json")), approval)
}
