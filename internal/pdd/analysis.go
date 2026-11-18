package pdd

import (
	"fmt"
	"path/filepath"
	"time"
)

type AgentFinding struct {
	Severity     string   `json:"severity"`
	Kind         string   `json:"kind"`
	Text         string   `json:"text"`
	Requirements []string `json:"requirements"`
}
type AgentResult struct {
	Schema        int            `json:"schema"`
	RequestDigest string         `json:"request_digest"`
	Status        string         `json:"status"`
	Summary       string         `json:"summary"`
	Findings      []AgentFinding `json:"findings"`
	PlanJSON      string         `json:"plan_json"`
	Edits         []SourceEdit   `json:"edits"`
}
type SourceEdit struct {
	File    string `json:"file"`
	Content string `json:"content"`
}
type AnalysisRecord struct {
	Schema        int         `json:"schema"`
	PlanID        string      `json:"plan_id"`
	Role          string      `json:"role"`
	SpecDigest    string      `json:"spec_digest"`
	SourceDigest  string      `json:"source_digest"`
	ReceiptDigest string      `json:"receipt_digest,omitempty"`
	Invocation    string      `json:"invocation"`
	Driver        string      `json:"driver"`
	CreatedAt     time.Time   `json:"created_at"`
	Result        AgentResult `json:"result"`
}

func analysisPath(root string, p Plan, role string) string {
	return filepath.Join(root, p.stateFile("analysis"), role+".json")
}
func validateAnalysis(p Plan, a AnalysisRecord, role string) error {
	if a.Schema != 1 || a.PlanID != p.ID() || a.Role != role || a.Invocation == "" || a.CreatedAt.IsZero() || a.Result.Schema != 1 || a.Result.RequestDigest == "" {
		return fmt.Errorf("invalid %s agent review", role)
	}
	if a.Result.Status != "ready" && a.Result.Status != "changes-required" && a.Result.Status != "needs-decision" && a.Result.Status != "unsupported" {
		return fmt.Errorf("invalid agent review status")
	}
	if a.Result.Summary == "" {
		return fmt.Errorf("agent review requires a summary")
	}
	reqs := map[string]bool{}
	for _, r := range p.Requirements {
		reqs[r.ID] = true
	}
	for _, f := range a.Result.Findings {
		if f.Severity != "blocker" && f.Severity != "warning" && f.Severity != "info" {
			return fmt.Errorf("unknown agent finding severity")
		}
		if f.Text == "" || f.Kind == "" {
			return fmt.Errorf("agent finding requires kind and explanation")
		}
		for _, id := range f.Requirements {
			if !reqs[id] {
				return fmt.Errorf("agent finding references unknown requirement %s", id)
			}
		}
		if a.Result.Status == "ready" && f.Severity == "blocker" {
			return fmt.Errorf("agent readiness contradicts a blocking finding")
		}
	}
	if len(a.Result.Edits) > 0 || a.Result.PlanJSON != "" {
		return fmt.Errorf("review agents must not return implementation edits or replace the plan")
	}
	return nil
}
func requirePlanningReviews(root string, p Plan) error {
	if p.Workflow == nil || !p.Workflow.DualReview {
		return nil
	}

	spec, err := specificationDigest(root, p)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	roles := map[string]bool{}
	for _, a := range p.PlanningReviews {
		if a.Role != "intent" && a.Role != "guarantees" {
			return fmt.Errorf("unexpected planning-review role")
		}
		if err = validateAnalysis(p, a, a.Role); err != nil {
			return err
		}
		if a.SpecDigest != spec || a.Result.Status != "ready" || seen[a.Invocation] || roles[a.Role] {
			return fmt.Errorf("two separate current planning reviews must be ready before human approval")
		}
		seen[a.Invocation] = true
		roles[a.Role] = true
	}
	if !roles["intent"] || !roles["guarantees"] {
		return fmt.Errorf("intent and guarantees reviews must both be ready before human approval")
	}

	return nil
}
func currentGapReview(root string, p Plan, receipt Receipt, receiptDir string) (*AnalysisRecord, error) {
	var a AnalysisRecord
	if err := ReadJSON(analysisPath(root, p, "gap"), &a); err != nil {
		return nil, err
	}
	if err := validateAnalysis(p, a, "gap"); err != nil {
		return nil, err
	}
	data, err := readArtifact(filepath.Join(receiptDir, "receipt.json"), 8<<20)
	if err != nil {
		return nil, err
	}
	if a.SpecDigest != receipt.PlanDigest || a.SourceDigest != receipt.SourceDigest || a.ReceiptDigest != hashBytes(data) {
		return nil, fmt.Errorf("gap review is stale for the current receipt")
	}
	return &a, nil
}
