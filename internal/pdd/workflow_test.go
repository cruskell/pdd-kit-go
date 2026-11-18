package pdd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A separate deterministic executable exercises the real process/JSON boundary.
// This is a protocol fixture, not a substitute for the verifier integration.
func TestWorkflowWorker(t *testing.T) {
	mode := os.Getenv("PDD_TEST_WORKER")
	if mode == "" {
		return
	}
	var req AgentRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	result := AgentResult{Schema: 1, RequestDigest: req.Digest, Status: "ready", Summary: "fixture review", Findings: []AgentFinding{}, Edits: []SourceEdit{}}
	switch mode {
	case "wrong-request":
		result.RequestDigest = "other"
	case "blocking":
		result.Findings = []AgentFinding{{Severity: "blocker", Kind: "intent", Text: "missing requirement"}}
	case "tamper":
		_ = os.WriteFile("core.go", []byte("package fixture\n"), 0644)
	case "repair", "weaken":
		if req.Role == "implement" {
			source := strings.Replace(req.Sources["core.go"], "return false", "return present && matches && !locked", 1)
			if mode == "weaken" {
				source = strings.Replace(source, "result == (present && matches && !locked)", "true", 1)
			}
			result.Edits = []SourceEdit{{File: "core.go", Content: source}}
		}
	case "plan":
		b, _ := json.Marshal(req.Plan)
		result.PlanJSON = string(b)
	case "decision":
		result.Status = "needs-decision"
		result.Summary = "scope must change"
	}
	if len(req.Plan.PlanningReviews) > 0 {
		fmt.Fprintln(os.Stderr, "review contexts were not separated")
		os.Exit(2)
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	os.Exit(0)
}
func workerOptions(t *testing.T, mode string) AgentOptions {
	t.Helper()
	t.Setenv("PDD_TEST_WORKER", mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return AgentOptions{Worker: []string{exe, "-test.run=^TestWorkflowWorker$"}, Timeout: 20 * time.Second}
}
func savePlan(t *testing.T, root string, p Plan) {
	t.Helper()
	if err := WriteJSON(filepath.Join(root, p.Paths().File), p); err != nil {
		t.Fatal(err)
	}
}
func reviewTwice(t *testing.T, root string, p Plan) Plan {
	t.Helper()
	opts := workerOptions(t, "review")
	for _, role := range []string{"intent", "guarantees"} {
		if _, err := Analyze(context.Background(), root, p, role, opts); err != nil {
			t.Fatal(err)
		}
		var err error
		p, err = LoadNamed(root, p.ID())
		if err != nil {
			t.Fatal(err)
		}
	}
	return p
}
func TestDecisionDraftAndExplicitClauseAcceptance(t *testing.T) {
	root, p := fixture(t)
	p.Decisions = []Decision{{ID: "direction", Question: "Both directions?", Context: "Reject-only permits deny-all", Options: []DecisionOption{{ID: "iff", Label: "Both", Consequence: "Accept all valid inputs"}, {ID: "only-if", Label: "Rejection only", Consequence: "May deny valid inputs"}}}}
	p.IntentMap = []IntentItem{{ID: "original", UserWords: "Accept iff valid and unlocked", Requirements: []string{"decision", "boundary"}, Decisions: []string{"direction"}}}
	p.Trust[0].Clauses = []AssumedClause{{ID: "truthful-input", Text: "identity input matches the caller", Impact: "impersonation may be accepted", Test: "TestBoundary"}}
	p.Scenarios = []Scenario{{ID: "caller-boundary", Check: "tests", Test: "TestBoundary", Given: "Caller inputs", When: "The boundary maps identity", Then: "Correct inputs reach the policy", Purpose: "boundary"}}
	savePlan(t, root, p)
	view, err := InspectDraft(root, "default")
	if err != nil || len(view.OpenDecisions) != 1 || view.ValidationError == "" {
		t.Fatalf("invalid draft missing: %+v %v", view, err)
	}
	if _, err = Approve(root, p, "human", []string{"identity", "truthful-input"}); err == nil {
		t.Fatal("unanswered decision approved")
	}
	if _, err = AnswerDecision(root, "default", "direction", "iff", "acceptance required", "stale"); err == nil {
		t.Fatal("stale answer applied")
	}
	p, err = AnswerDecision(root, "default", "direction", "iff", "acceptance required", view.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Approve(root, p, "human", []string{"identity"}); err == nil {
		t.Fatal("individual clause not accepted")
	}
	if _, err = Approve(root, p, "human", []string{"identity", "truthful-input"}); err != nil {
		t.Fatal(err)
	}
	if err = CheckApproval(root, p); err != nil {
		t.Fatal(err)
	}
}
func TestIndependentPlanningReviewsAreApprovalBound(t *testing.T) {
	root, p := fixture(t)
	p.Workflow = &WorkflowPolicy{DualReview: true}
	addPlanningMetadata(&p)
	savePlan(t, root, p)
	if _, err := Approve(root, p, "human", []string{"identity"}); err == nil {
		t.Fatal("unreviewed plan approved")
	}
	p = reviewTwice(t, root, p)
	if p.PlanningReviews[0].Invocation == p.PlanningReviews[1].Invocation {
		t.Fatal("same reviewer context")
	}
	approveFixture(t, root, p)
	p.PlanningReviews[0].Result.Summary = "edited opinion"
	savePlan(t, root, p)
	if CheckApproval(root, p) == nil {
		t.Fatal("review edits retained approval")
	}
	p.Intent = "different intent"
	savePlan(t, root, p)
	if _, err := Approve(root, p, "human", []string{"identity"}); err == nil {
		t.Fatal("stale planning reviews approved")
	}
}
func TestWorkerRejectsMisboundContradictoryAndMutatingReviews(t *testing.T) {
	for _, mode := range []string{"wrong-request", "blocking", "tamper"} {
		t.Run(mode, func(t *testing.T) {
			root, p := fixture(t)
			if _, err := Analyze(context.Background(), root, p, "intent", workerOptions(t, mode)); err == nil {
				t.Fatal("invalid worker accepted")
			}
			b, _ := os.ReadFile(filepath.Join(root, "core.go"))
			if string(b) != coreSource {
				t.Fatal("worker changed real source")
			}
		})
	}
}
func TestMutableProofHintsKeepPublicContractsFrozen(t *testing.T) {
	root, p := fixture(t)
	p.Workflow = &WorkflowPolicy{MutableProofHints: true}
	savePlan(t, root, p)
	approveFixture(t, root, p)
	body := strings.Replace(coreSource, " return", " // @ assert present == present\n return", 1)
	if err := os.WriteFile(filepath.Join(root, "core.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CheckApproval(root, p); err != nil {
		t.Fatalf("provable hint invalidated approval: %v", err)
	}
	body = strings.Replace(body, "// @ ensures result == (present && matches && !locked)", "// @ ensures true", 1)
	_ = os.WriteFile(filepath.Join(root, "core.go"), []byte(body), 0644)
	if CheckApproval(root, p) == nil {
		t.Fatal("weaker public guarantee retained approval")
	}
}
func stablePlan(t *testing.T) (string, Plan) {
	root, p := fixture(t)
	p.Workflow = &WorkflowPolicy{MutableProofHints: true, GapReview: true}
	p.Mutations[0].Kind = "last-return"
	p.Mutations[0].Find = ""
	savePlan(t, root, p)
	return root, p
}
func TestBuildRepairsVerifiesAndBindsGapReview(t *testing.T) {
	root, p := stablePlan(t)
	approveFixture(t, root, p)
	broken := strings.Replace(coreSource, "return present && matches && !locked", "return false", 1)
	_ = os.WriteFile(filepath.Join(root, "core.go"), []byte(broken), 0644)
	opts := BuildOptions{Agent: workerOptions(t, "repair"), Engine: Options{Runner: &fakeRunner{}}, Iterations: 2}
	dry, err := Build(context.Background(), root, p, opts)
	if err != nil || !dry.Success || dry.Applied || dry.Iterations != 1 {
		t.Fatalf("dry build: %+v %v", dry, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "core.go"))
	if string(b) != broken {
		t.Fatal("dry build changed production source")
	}
	opts.Apply = true
	opts.Engine.Runner = &fakeRunner{}
	result, err := Build(context.Background(), root, p, opts)
	if err != nil || !result.Success || !result.Applied || result.Stage != "ready" {
		t.Fatalf("build: %+v %v", result, err)
	}
	view, err := WorkflowReport(context.Background(), root, p, "")
	if err != nil || !view.ReadyForPR {
		t.Fatalf("workflow: %+v %v", view, err)
	}
	preview, err := PreparePR(context.Background(), root, p, "", PROptions{})
	if err != nil || !strings.Contains(preview.Body, "Advisory gap review") {
		t.Fatalf("PR: %+v %v", preview, err)
	}
	if _, err = PreparePR(context.Background(), root, p, "", PROptions{Create: true}); err == nil {
		t.Fatal("uncommitted non-git candidate published")
	}
	if _, _, err = Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}}); err != nil {
		t.Fatal(err)
	}
	view, err = WorkflowReport(context.Background(), root, p, "")
	if err != nil || view.ReadyForPR {
		t.Fatal("old gap review reused for a different receipt")
	}
}
func TestBuildStopsAtScopeChangeAndPreservesSource(t *testing.T) {
	root, p := stablePlan(t)
	approveFixture(t, root, p)
	broken := strings.Replace(coreSource, "return present && matches && !locked", "return false", 1)
	_ = os.WriteFile(filepath.Join(root, "core.go"), []byte(broken), 0644)
	result, err := Build(context.Background(), root, p, BuildOptions{Apply: true, Agent: workerOptions(t, "weaken"), Engine: Options{Runner: &fakeRunner{}}, Iterations: 2})
	if err != nil || result.Success || !result.NeedsDecision {
		t.Fatalf("scope change: %+v %v", result, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "core.go"))
	if string(b) != broken {
		t.Fatal("unapproved source was installed")
	}
}
func TestPlanningDoesNotChangeImplementationOrApprove(t *testing.T) {
	root, p := fixture(t)
	if err := applySourceEdits(root, []SourceEdit{{File: "core.go", Content: strings.Replace(coreSource, "return present && matches && !locked", "return false", 1)}}, true); err == nil {
		t.Fatal("planner changed code")
	}
	view, _ := InspectDraft(root, "default")
	result, err := GenerateDraft(context.Background(), root, "default", "Accept iff valid and unlocked", view.Digest, true, workerOptions(t, "plan"))
	if err != nil || !result.Applied {
		t.Fatalf("draft: %+v %v", result, err)
	}
	if CheckApproval(root, result.View.Plan) == nil {
		t.Fatal("planner approved intent")
	}
	if !result.View.Plan.Workflow.DualReview || !result.View.Plan.Workflow.GapReview {
		t.Fatal("generated draft skipped reviews")
	}
	_ = p
}
func TestDashboardEscapesRepositoryText(t *testing.T) {
	root, p := fixture(t)
	p.Intent = "<script>alert(1)</script>"
	savePlan(t, root, p)
	path := filepath.Join(t.TempDir(), "review.html")
	if err := WriteDashboard(context.Background(), root, "default", "", path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "<script>") || !strings.Contains(string(b), "&lt;script&gt;") {
		t.Fatal("unsafe dashboard rendering")
	}
}
func TestGobraWorkflowIntegration(t *testing.T) {
	if os.Getenv("PDD_INTEGRATION") != "1" {
		t.Skip("set PDD_INTEGRATION=1 for real build/repair/proof/gap workflow")
	}
	root, p := stablePlan(t)
	p.Workflow.DualReview = true
	addPlanningMetadata(&p)
	savePlan(t, root, p)
	p = reviewTwice(t, root, p)
	approveFixture(t, root, p)
	broken := strings.Replace(coreSource, "return present && matches && !locked", "return false", 1)
	_ = os.WriteFile(filepath.Join(root, "core.go"), []byte(broken), 0644)
	result, err := Build(context.Background(), root, p, BuildOptions{Apply: true, Iterations: 2, Agent: workerOptions(t, "repair"), Engine: Options{Runner: ToolRunner{Timeout: 2 * time.Minute}}})
	if err != nil || !result.Success {
		t.Fatalf("real build: %+v %v", result, err)
	}
	view, err := WorkflowReport(context.Background(), root, p, "")
	if err != nil || !view.ReadyForPR {
		t.Fatalf("real ready: %+v %v", view, err)
	}
}

func TestPlanningAllowsCommentOnlyContractEdits(t *testing.T) {
	root, _ := fixture(t)
	source := strings.Replace(coreSource, "// @ ensures", "// Contract explanation\n// @ ensures", 1)
	if err := applySourceEdits(root, []SourceEdit{{File: "core.go", Content: source}}, true); err != nil {
		t.Fatal(err)
	}
}
func TestBuildReturnsUnresolvedGapWithoutInstalling(t *testing.T) {
	root, p := stablePlan(t)
	approveFixture(t, root, p)
	result, err := Build(context.Background(), root, p, BuildOptions{Apply: true, Agent: workerOptions(t, "decision"), Engine: Options{Runner: &fakeRunner{}}})
	if err != nil || result.Success || !result.NeedsDecision || result.Applied {
		t.Fatalf("unresolved gap: %+v %v", result, err)
	}
}
func TestCandidateInstallationRejectsConcurrentSourceChange(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	base, err := Capture(root)
	if err != nil {
		t.Fatal(err)
	}
	candidate := base
	var approval Approval
	if err = ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &approval); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "core.go"), []byte(strings.Replace(coreSource, "return present && matches && !locked", "return false", 1)), 0644)
	if installCandidate(root, p, base, candidate, nil, "", &approval) == nil {
		t.Fatal("concurrent source overwritten")
	}
}
func TestLastReturnIgnoresNestedClosure(t *testing.T) {
	source := strings.Replace(coreSource, " return present", " _ = func() bool { return true }\n return present", 1)
	m := Mutation{ID: "negative", Kind: "last-return", File: "core.go", Replace: "return false"}
	mutated, err := Mutate(m, Requirement{Function: "Decide"}, []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mutated), "func() bool { return true }") || !strings.Contains(string(mutated), "\n return false") {
		t.Fatalf("wrong return replaced: %s", mutated)
	}
}

func addPlanningMetadata(p *Plan) {
	p.IntentMap = []IntentItem{{ID: "original-request", UserWords: p.Intent, Requirements: []string{"decision", "boundary"}}}
	p.Trust[0].Affects = []string{"decision"}
	p.Trust[0].Clauses = []AssumedClause{{ID: "caller-inputs", Text: "Boundary supplies correct input tuple", Impact: "Wrong identity may be authorized", Test: "TestBoundary"}}
	p.Scenarios = []Scenario{{ID: "caller-scenario", Check: "tests", Test: "TestBoundary", Given: "Caller state", When: "The policy is invoked", Then: "Tuple is forwarded correctly", Purpose: "boundary"}}
}
