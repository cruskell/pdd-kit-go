package pdd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BuildTask struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	DependsOn    []string `json:"depends_on"`
	Requirements []string `json:"requirements"`
	Trust        []string `json:"trust"`
	Status       string   `json:"status"`
}
type WorkflowView struct {
	receipt         *Receipt
	receiptDir      string
	PlanID          string          `json:"plan_id"`
	Digest          string          `json:"digest"`
	Tasks           []BuildTask     `json:"tasks"`
	MachineCurrent  bool            `json:"machine_current"`
	MachineSuccess  bool            `json:"machine_success"`
	GapReview       *AnalysisRecord `json:"gap_review,omitempty"`
	ReadyForPR      bool            `json:"ready_for_pr"`
	Gaps            []string        `json:"gaps"`
	PruneCandidates []string        `json:"test_prune_candidates"`
}

func WorkflowReport(ctx context.Context, root string, p Plan, policy string) (WorkflowView, error) {
	view := WorkflowView{PlanID: p.ID(), Tasks: []BuildTask{}, Gaps: []string{}, PruneCandidates: []string{}}
	digest, err := Digest(root, p)
	if err != nil {
		return view, err
	}
	view.Digest = digest
	graph, err := resolvePackages(root, p)
	if err != nil {
		return view, err
	}
	receipt, dir, statusErr := StatusContext(ctx, root, p, policy)
	view.MachineCurrent = statusErr == nil
	view.MachineSuccess = statusErr == nil && receipt.Success
	states := map[string]string{}
	if statusErr == nil {
		view.receipt, view.receiptDir = &receipt, dir
		for _, e := range receipt.Evidence {
			states[e.Check] = e.Execution.Status
		}
	}
	for _, c := range graph.Order {
		task := BuildTask{ID: c.ID, Kind: c.Kind, DependsOn: append([]string{}, graph.Dependencies[c.ID]...), Requirements: []string{}, Trust: []string{}, Status: "pending"}
		if state, ok := states[c.ID]; ok {
			task.Status = state
		}
		for _, r := range p.Requirements {
			if r.Check == c.ID {
				task.Requirements = append(task.Requirements, r.ID)
			}
		}
		for _, t := range p.Trust {
			if t.Check == c.ID {
				task.Trust = append(task.Trust, t.ID)
			}
		}
		view.Tasks = append(view.Tasks, task)
	}
	if statusErr != nil {
		view.Gaps = append(view.Gaps, statusErr.Error())
	} else if !receipt.Success {
		view.Gaps = append(view.Gaps, receipt.Gaps...)
	}
	if p.Workflow != nil && p.Workflow.DualReview {
		if err = requirePlanningReviews(root, p); err != nil {
			view.Gaps = append(view.Gaps, err.Error())
		}
	}
	if statusErr == nil && p.Workflow != nil && p.Workflow.GapReview {
		review, err := currentGapReview(root, p, receipt, dir)
		if err != nil {
			view.Gaps = append(view.Gaps, "current advisory gap review is required: "+err.Error())
		} else {
			view.GapReview = review
			if review.Result.Status != "ready" {
				view.Gaps = append(view.Gaps, "gap reviewer: "+review.Result.Status+" "+review.Result.Summary)
			}
		}
	}
	view.ReadyForPR = view.MachineSuccess && len(view.Gaps) == 0
	if view.MachineSuccess {
		formal := map[string]bool{}
		for _, v := range receipt.Requirements {
			formal[v.ID] = v.Kind == "formal" && v.Status == "proved"
		}
		for _, scenario := range p.Scenarios {
			if scenario.Purpose != "formal-regression" || len(scenario.Requirements) == 0 {
				continue
			}
			candidate := true
			for _, other := range p.Scenarios {
				if other.Check == scenario.Check && other.Test == scenario.Test && other.Purpose != "formal-regression" {
					candidate = false
				}
			}
			for _, requirement := range p.Requirements {
				if requirement.Kind == "empirical" && requirement.Check == scenario.Check {
					candidate = false
				}
			}
			for _, id := range scenario.Requirements {
				candidate = candidate && formal[id]
			}
			for _, t := range p.Trust {
				for _, clause := range t.Clauses {
					if t.Check == scenario.Check && clause.Test == scenario.Test {
						candidate = false
					}
				}
				if t.Check == scenario.Check && len(t.Clauses) == 0 {
					candidate = false
				}
			}
			if candidate {
				view.PruneCandidates = append(view.PruneCandidates, scenario.ID)
			}
		}
	}
	return view, nil
}

type BuildOptions struct {
	Agent      AgentOptions
	Engine     Options
	Iterations int
	Apply      bool
}
type BuildOutcome struct {
	PlanID        string          `json:"plan_id"`
	Success       bool            `json:"success"`
	Stage         string          `json:"stage"`
	NeedsDecision bool            `json:"needs_decision"`
	Applied       bool            `json:"applied"`
	Iterations    int             `json:"iterations"`
	Artifacts     string          `json:"artifacts"`
	Receipt       *Receipt        `json:"machine_receipt,omitempty"`
	GapReview     *AnalysisRecord `json:"gap_review,omitempty"`
	Changes       []SourceEdit    `json:"changes"`
	Message       string          `json:"message"`
}

func copyArtifactDirectory(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := readArtifact(filepath.Join(source, entry.Name()), (8<<20)+1024)
		if err != nil {
			return err
		}
		if err = AtomicWrite(filepath.Join(destination, entry.Name()), data); err != nil {
			return err
		}
	}
	return nil
}
func Build(ctx context.Context, root string, p Plan, opts BuildOptions) (out BuildOutcome, err error) {
	out = BuildOutcome{PlanID: p.ID(), Stage: "approval", Changes: []SourceEdit{}}
	limit := opts.Iterations
	if limit == 0 {
		limit = 3
	}
	if limit < 1 || limit > 10 {
		return out, fmt.Errorf("build iterations must be 1..10")
	}
	unlock, err := lockProject(root)
	if err != nil {
		return out, err
	}
	if err = p.Validate(root); err != nil {
		unlock()
		return out, err
	}
	if err = CheckApproval(root, p); err != nil {
		unlock()
		return out, err
	}
	var approval Approval
	if err = ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &approval); err != nil {
		unlock()
		return out, err
	}
	if approval.Signature != nil && opts.Engine.PolicyPath == "" {
		unlock()
		return out, fmt.Errorf("signed build requires --policy")
	}
	if opts.Engine.PolicyPath != "" {
		if _, err = verifyPolicy(opts.Engine.PolicyPath, p, approval, time.Now().UTC()); err != nil {
			unlock()
			return out, err
		}
	}
	expected, err := Digest(root, p)
	if err != nil {
		unlock()
		return out, err
	}
	base, err := CaptureContext(ctx, root, p.Inputs)
	unlock()
	if err != nil {
		return out, err
	}
	out.Artifacts = filepath.Join(root, p.stateFile("builds"), time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err = os.MkdirAll(out.Artifacts, 0700); err != nil {
		return out, err
	}
	defer func() {
		if err != nil {
			out.Success = false
		}
		if saveErr := WriteJSON(filepath.Join(out.Artifacts, "build.json"), out); err == nil && saveErr != nil {
			out.Success = false
			err = saveErr
		}
	}()
	scratch, err := createScratch("pdd-build-")
	if err != nil {
		return out, err
	}
	defer removeScratch(scratch)
	if err = base.MaterializeContext(ctx, scratch); err != nil {
		return out, err
	}
	if err = WriteJSON(filepath.Join(scratch, p.Paths().File), p); err != nil {
		return out, err
	}
	if err = WriteJSON(filepath.Join(scratch, p.stateFile("approval.json")), approval); err != nil {
		return out, err
	}
	var last *Receipt
	for attempt := 0; attempt <= limit; attempt++ {
		if err = ctx.Err(); err != nil {
			return out, err
		}
		digest, e := Digest(scratch, p)
		if e != nil || digest != expected {
			out.NeedsDecision = true
			out.Stage = "needs-decision"
			out.Message = "contracts, pure definitions, declarations or trust scope changed; human review is required"
			return out, nil
		}
		out.Stage = "machine-verification"
		receipt, dir, proofErr := Prove(ctx, scratch, p, opts.Engine)
		last = &receipt
		out.Receipt = last
		if dir != "" {
			if e = copyArtifactDirectory(dir, filepath.Join(out.Artifacts, fmt.Sprintf("iteration-%d", attempt))); e != nil {
				return out, e
			}
		}
		machineReady := proofErr == nil && receipt.Success
		if machineReady {
			out.Stage = "gap-review"
			record, e := runAgent(ctx, scratch, p, "gap", "", last, opts.Agent)
			if e != nil {
				return out, e
			}
			data, e := readArtifact(filepath.Join(dir, "receipt.json"), 8<<20)
			if e != nil {
				return out, e
			}
			record.ReceiptDigest = hashBytes(data)
			out.GapReview = &record
			if e = WriteJSON(analysisPath(scratch, p, "gap"), record); e != nil {
				return out, e
			}
			if e = WriteJSON(filepath.Join(out.Artifacts, fmt.Sprintf("gap-%d.json", attempt)), record); e != nil {
				return out, e
			}
			if record.Result.Status == "ready" {
				out.Success = true
				break
			}
			if record.Result.Status == "needs-decision" || record.Result.Status == "unsupported" {
				out.NeedsDecision = true
				out.Stage = "needs-decision"
				out.Message = record.Result.Summary
				return out, nil
			}
		}
		for _, e := range receipt.Evidence {
			if e.Execution.Status == "unavailable" {
				out.Stage = "tools-unavailable"
				out.Message = e.Execution.Detail
				return out, nil
			}
		}
		if attempt == limit {
			out.Stage = "gaps-open"
			out.Message = "repair limit reached; machine or advisory gaps remain"
			return out, nil
		}
		out.Stage = "implementation"
		goal := "Repair failing machine checks and advisory findings without changing approved intent."
		if proofErr != nil {
			goal += " Validation error: " + proofErr.Error()
		}
		if out.GapReview != nil {
			goal += " Gap review: " + out.GapReview.Result.Summary
		}
		record, e := runAgent(ctx, scratch, p, "implement", goal, last, opts.Agent)
		if e != nil {
			return out, e
		}
		if e = WriteJSON(filepath.Join(out.Artifacts, fmt.Sprintf("implementation-%d.json", attempt)), record); e != nil {
			return out, e
		}
		if record.Result.Status == "needs-decision" || record.Result.Status == "unsupported" || record.Result.PlanJSON != "" {
			out.NeedsDecision = true
			out.Stage = "needs-decision"
			out.Message = record.Result.Summary
			return out, nil
		}
		if len(record.Result.Edits) == 0 {
			out.Stage = "gaps-open"
			out.Message = "agent returned no repair"
			return out, nil
		}
		if e = applySourceEdits(scratch, record.Result.Edits, false); e != nil {
			return out, e
		}
		out.Iterations++
	}
	if !out.Success {
		return out, nil
	}
	if _, _, err = StatusContext(ctx, scratch, p, opts.Engine.PolicyPath); err != nil {
		return out, err
	}
	candidate, err := CaptureContext(ctx, scratch, p.Inputs)
	if err != nil {
		return out, err
	}
	for file, data := range candidate.Files {
		if previous, ok := base.Files[file]; !ok || !bytesEqual(previous, data) {
			out.Changes = append(out.Changes, SourceEdit{File: file, Content: string(data)})
		}
	}
	sort.Slice(out.Changes, func(i, j int) bool { return out.Changes[i].File < out.Changes[j].File })
	if err = WriteJSON(filepath.Join(out.Artifacts, "changes.json"), out.Changes); err != nil {
		return out, err
	}
	if !opts.Apply {
		out.Stage = "verified-candidate"
		out.Message = "candidate verified; no production files changed (use --apply to install)"
		return out, nil
	}
	if opts.Engine.PolicyPath != "" {
		if _, err = verifyPolicy(opts.Engine.PolicyPath, p, approval, time.Now().UTC()); err != nil {
			out.Success = false
			return out, err
		}
	}
	if err = installCandidate(root, p, base, candidate, nil, "", &approval); err != nil {
		out.Success = false
		return out, err
	}
	out.Applied = true
	out.Stage = "final-verification"
	final, dir, err := Prove(ctx, root, p, opts.Engine)
	out.Receipt = &final
	if err != nil || !final.Success {
		out.Success = false
		out.Message = "installed candidate did not produce a successful final receipt"
		return out, err
	}
	data, err := readArtifact(filepath.Join(dir, "receipt.json"), 8<<20)
	if err != nil {
		out.Success = false
		return out, err
	}
	gap := *out.GapReview
	gap.ReceiptDigest = hashBytes(data)
	gap.SpecDigest = final.PlanDigest
	gap.SourceDigest = final.SourceDigest
	if err = WriteJSON(analysisPath(root, p, "gap"), gap); err != nil {
		out.Success = false
		return out, err
	}
	out.GapReview = &gap
	out.Stage = "ready"
	out.Message = "approved contracts preserved; production files, machine receipt and advisory gap review are current"
	return out, nil
}
func bytesEqual(a, b []byte) bool { return string(a) == string(b) }

func summaryFindings(findings []AgentFinding) string {
	parts := []string{}
	for _, f := range findings {
		if f.Severity == "blocker" {
			parts = append(parts, f.Text)
		}
	}
	return strings.Join(parts, "; ")
}
