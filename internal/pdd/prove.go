package pdd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type EvidenceArtifact struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type Evidence struct {
	Artifacts    []EvidenceArtifact `json:"artifacts,omitempty"`
	Check        string             `json:"check"`
	Kind         string             `json:"kind"` // formal or empirical
	Execution    Execution          `json:"execution"`
	Log          string             `json:"log"`
	LogSHA256    string             `json:"log_sha256"`
	Dependencies []string           `json:"dependencies,omitempty"`
}
type MutationResult struct {
	ID          string    `json:"id"`
	Requirement string    `json:"requirement"`
	Status      string    `json:"status"` // killed, survived, inconclusive, not-run
	Execution   Execution `json:"execution"`
	Log         string    `json:"log,omitempty"`
	LogSHA256   string    `json:"log_sha256,omitempty"`
}
type Verdict struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Status string `json:"status"` // proved, evidenced, unproven
	Check  string `json:"check"`
}
type Receipt struct {
	ApprovalPolicy *PolicyEvidence   `json:"approval_policy,omitempty"`
	Inputs         *InputRecord      `json:"inputs,omitempty"`
	PlanID         string            `json:"plan_id,omitempty"`
	Schema         int               `json:"schema"`
	Version        string            `json:"version"`
	StartedAt      time.Time         `json:"started_at"`
	FinishedAt     time.Time         `json:"finished_at"`
	PlanDigest     string            `json:"plan_digest"`
	SourceDigest   string            `json:"source_digest"`
	GitCommit      string            `json:"git_commit,omitempty"`
	Approval       Approval          `json:"approval"`
	Tools          map[string]string `json:"tools"`
	Success        bool              `json:"success"`
	Evidence       []Evidence        `json:"evidence"`
	Mutations      []MutationResult  `json:"mutations"`
	Requirements   []Verdict         `json:"requirements"`
	Trust          []Trust           `json:"trusted_boundary"`
	NonGoals       []string          `json:"non_goals"`
	Gaps           []string          `json:"gaps"`
}
type Options struct {
	PolicyPath string
	Version    string
	Runner     Runner
	Tools      map[string]string
	Progress   func(string)
}

// Prove executes only approved checks. Missing tools, missing tests, surviving
// mutants and changes during verification all fail closed and produce a receipt.
func Prove(ctx context.Context, root string, p Plan, options Options) (Receipt, string, error) {
	receipt := Receipt{PlanID: p.ID(), Schema: 1, Version: options.Version, StartedAt: time.Now().UTC(), Evidence: []Evidence{}, Mutations: []MutationResult{}, Requirements: []Verdict{}, Trust: p.Trust, NonGoals: p.NonGoals, Gaps: []string{}, Tools: options.Tools}
	unlock, lockErr := lockProject(root)
	if lockErr != nil {
		return receipt, "", lockErr
	}
	defer unlock()
	if err := p.Validate(root); err != nil {
		return receipt, "", err
	}
	if err := CheckApproval(root, p); err != nil {
		return receipt, "", err
	}
	graph, err := resolvePackages(root, p)
	if err != nil {
		return receipt, "", err
	}
	if err := ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &receipt.Approval); err != nil {
		return receipt, "", err
	}
	digest, err := Digest(root, p)
	if err != nil {
		return receipt, "", err
	}
	if receipt.Approval.Signature != nil && options.PolicyPath == "" {
		return receipt, "", fmt.Errorf("signed approval requires --policy for authorization")
	}
	if options.PolicyPath != "" {
		evidence, err := verifyPolicy(options.PolicyPath, p, receipt.Approval, time.Now().UTC())
		if err != nil {
			return receipt, "", err
		}
		receipt.ApprovalPolicy = &evidence
	}
	receipt.PlanDigest = digest
	if b, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output(); err == nil {
		receipt.GitCommit = strings.TrimSpace(string(b))
	}
	// In-process callers cannot claim proof without a real backend. Tests inject a
	// runner to exercise orchestration; only ToolRunner is exposed by the CLI.
	runner := options.Runner
	if runner == nil {
		runner = ToolRunner{}
	}
	dir := filepath.Join(root, p.stateFile("receipts"), receipt.StartedAt.Format("20060102T150405.000000000Z"))
	if err = NoSymlinks(dir); err != nil {
		return receipt, "", err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return receipt, "", err
	}
	state := RunState{PlanID: p.ID(), Schema: 1, ID: filepath.Base(dir), State: "running", StartedAt: receipt.StartedAt}
	if err = WriteJSON(filepath.Join(root, p.stateFile("run.json")), state); err != nil {
		return receipt, "", err
	}
	completed := false
	defer func() {
		if !completed {
			state.State = "interrupted"
			state.FinishedAt = time.Now().UTC()
			state.Detail = "verification exited without a completed receipt"
			_ = WriteJSON(filepath.Join(root, p.stateFile("run.json")), state)
		}
	}()
	source, err := CaptureContext(ctx, root, p.Inputs)
	if err != nil {
		return receipt, "", err
	}
	receipt.SourceDigest = source.Hash
	inputRecord := source.Record()
	receipt.Inputs = &inputRecord
	if err = WriteJSON(filepath.Join(dir, "inputs.json"), source.Manifest); err != nil {
		return receipt, "", err
	}
	manifestBytes, err := readArtifact(filepath.Join(dir, "inputs.json"), 8<<20)
	if err != nil {
		return receipt, "", err
	}
	receipt.Inputs.ManifestSHA256 = hashBytes(manifestBytes)
	scratch, err := createScratch("pdd-baseline-")
	if err != nil {
		return receipt, "", err
	}
	defer removeScratch(scratch)
	progress := options.Progress
	if progress == nil {
		progress = func(string) {}
	}
	checks := map[string]Check{}
	executions := map[string]Execution{}
	saveLog := func(name string, e Execution) (string, string, error) {
		name += ".log"
		if err := AtomicWrite(filepath.Join(dir, name), []byte(e.Output)); err != nil {
			return "", "", err
		}
		return name, hashBytes([]byte(e.Output)), nil
	}
	recordExecution := func(c Check, e Execution) error {
		var artifacts []EvidenceArtifact
		var names []string
		for name := range e.artifacts {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			data := e.artifacts[name]
			if !validBrowserArtifactName(c, name) || len(data) > browserArtifactLimit {
				return fmt.Errorf("invalid browser artifact %s", name)
			}
			if err := AtomicWrite(filepath.Join(dir, name), data); err != nil {
				return err
			}
			artifacts = append(artifacts, EvidenceArtifact{File: name, SHA256: hashBytes(data)})
		}
		e.artifacts = nil
		executions[c.ID] = e
		log, hash, err := saveLog(c.ID, e)
		if err != nil {
			return err
		}
		kind := "empirical"
		if c.Kind == "gobra" {
			kind = "formal"
		}
		receipt.Evidence = append(receipt.Evidence, Evidence{Check: c.ID, Kind: kind, Execution: e, Log: log, LogSHA256: hash, Dependencies: graph.Dependencies[c.ID], Artifacts: artifacts})
		if e.Status != "passed" {
			receipt.Gaps = append(receipt.Gaps, c.ID+": "+e.Status+" "+e.Detail)
		}
		return nil
	}
	for _, c := range graph.Order {
		checks[c.ID] = c
		progress("Checking " + c.ID + " (" + c.Kind + ")")
		if ctx.Err() != nil {
			if err = recordExecution(c, Execution{Status: "canceled", ExitCode: -1, Detail: ctx.Err().Error()}); err != nil {
				return receipt, "", err
			}
			continue
		}
		if err = removeScratch(scratch); err != nil {
			return receipt, "", err
		}
		if err = os.MkdirAll(scratch, 0755); err != nil {
			return receipt, "", err
		}
		if err = source.MaterializeContext(ctx, scratch); err != nil {
			return receipt, "", err
		}
		var blocked []string
		for _, dep := range graph.Dependencies[c.ID] {
			if executions[dep].Status != "passed" {
				blocked = append(blocked, dep)
			}
		}
		e := Execution{Status: "blocked", ExitCode: -1, Detail: "unproven dependencies: " + strings.Join(blocked, ", ")}
		if len(blocked) == 0 {
			if ctx.Err() != nil {
				e = Execution{Status: "canceled", ExitCode: -1, Detail: ctx.Err().Error()}
			} else {
				e = runner.Run(ctx, scratch, c)
			}
		}
		after, captureErr := CaptureContext(ctx, scratch, p.Inputs)
		if captureErr != nil {
			if ctx.Err() == nil {
				e.Status = "failed"
				e.Detail = "could not confirm check input snapshot: " + captureErr.Error()
			}
		} else if after.Hash != source.Hash {
			e.Status = "failed"
			e.Detail = "check modified its source snapshot"
		}
		if err = recordExecution(c, e); err != nil {
			return receipt, "", err
		}
	}
	// Tests/verifiers may write generated files; always build each mutant from the
	// captured input, never from a prior tool's modified checkout.
	reqs := map[string]Requirement{}
	for _, r := range p.Requirements {
		reqs[r.ID] = r
	}
	killed := map[string]bool{}
	for _, m := range p.Mutations {
		mr := MutationResult{ID: m.ID, Requirement: m.Requirement, Status: "not-run"}
		if executions[m.Check].Status == "passed" && ctx.Err() == nil {
			progress("Mutating " + m.ID)
			temp, err := createScratch("pdd-mutant-")
			if err != nil {
				return receipt, "", err
			}
			err = source.MaterializeContext(ctx, temp)
			if err == nil {
				var b []byte
				b, err = Mutate(m, reqs[m.Requirement], source.Files[m.File])
				if err == nil {
					target := filepath.Join(temp, filepath.FromSlash(m.File))
					err = os.Chmod(target, 0644)
					if err == nil {
						err = os.WriteFile(target, b, 0644)
					}
				}
			}
			if err != nil {
				removeScratch(temp)
				return receipt, "", err
			}
			mr.Execution = runner.Run(ctx, temp, checks[m.Check])
			removeScratch(temp)
			mr.Status = "inconclusive"
			if mr.Execution.Status == "passed" {
				mr.Status = "survived"
			} else if RejectedMutation(mr.Execution, reqs[m.Requirement], source.Files[m.File]) {
				mr.Status = "killed"
			}
			mr.Log, mr.LogSHA256, err = saveLog(m.ID, mr.Execution)
			if err != nil {
				return receipt, "", err
			}
		}
		killed[m.ID] = mr.Status == "killed"
		if mr.Status != "killed" {
			receipt.Gaps = append(receipt.Gaps, m.ID+": mutation "+mr.Status)
		}
		receipt.Mutations = append(receipt.Mutations, mr)
	}
	ready := map[string]bool{}
	for _, c := range graph.Order {
		ready[c.ID] = executions[c.ID].Status == "passed"
		for _, m := range p.Mutations {
			if m.Check == c.ID && !killed[m.ID] {
				ready[c.ID] = false
			}
		}
		for _, dep := range graph.Dependencies[c.ID] {
			if !ready[dep] {
				if ready[c.ID] {
					receipt.Gaps = append(receipt.Gaps, c.ID+": dependency assurance incomplete: "+dep)
				}
				ready[c.ID] = false
			}
		}
	}
	for _, r := range p.Requirements {
		v := Verdict{r.ID, r.Kind, "unproven", r.Check}
		if ready[r.Check] {
			if r.Kind == "empirical" {
				v.Status = "evidenced"
			} else {
				all := true
				for _, m := range p.Mutations {
					if m.Requirement == r.ID && !killed[m.ID] {
						all = false
					}
				}
				if all {
					v.Status = "proved"
				}
			}
		}
		receipt.Requirements = append(receipt.Requirements, v)
	}
	// Detect changes in original source, plan, approval, or frozen annotations.
	inputAuditIncomplete := false
	current, err := CaptureContext(ctx, root, p.Inputs)
	if err != nil {
		inputAuditIncomplete = true
		receipt.Gaps = append(receipt.Gaps, "could not confirm current inputs: "+err.Error())
	} else if current.Hash != source.Hash {
		receipt.Gaps = append(receipt.Gaps, "source changed during verification")
	}
	latest, err := LoadNamed(root, p.ID())
	if err != nil {
		receipt.Gaps = append(receipt.Gaps, "plan changed during verification")
	} else if d, e := Digest(root, latest); e != nil || d != digest {
		receipt.Gaps = append(receipt.Gaps, "plan or contracts changed during verification")
	}
	var finalApproval Approval
	approvalErr := ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &finalApproval)
	if CheckApproval(root, p) != nil || approvalErr != nil || !sameApproval(receipt.Approval, finalApproval) {
		receipt.Gaps = append(receipt.Gaps, "approval changed during verification")
	}
	if options.PolicyPath != "" {
		evidence, err := verifyPolicy(options.PolicyPath, p, finalApproval, time.Now().UTC())
		if err != nil || receipt.ApprovalPolicy == nil || evidence != *receipt.ApprovalPolicy {
			inputAuditIncomplete = true
			receipt.Gaps = append(receipt.Gaps, "review policy changed or authorization expired during verification")
		}
	}
	if ctx.Err() != nil {
		receipt.Gaps = append(receipt.Gaps, "verification canceled: "+ctx.Err().Error())
		for i := range receipt.Requirements {
			receipt.Requirements[i].Status = "unproven"
		}
	}
	receipt.Success = len(receipt.Gaps) == 0
	receipt.FinishedAt = time.Now().UTC()
	if !receipt.Success {
		for i := range receipt.Requirements {
			if inputAuditIncomplete || strings.Contains(strings.Join(receipt.Gaps, " "), "changed during verification") {
				receipt.Requirements[i].Status = "unproven"
			}
		}
	}
	if err = WriteJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		return receipt, "", err
	}
	if err = AtomicWrite(filepath.Join(dir, "receipt.md"), []byte(Markdown(receipt))); err != nil {
		return receipt, "", err
	}
	state.State = "completed"
	state.FinishedAt = receipt.FinishedAt
	if err = WriteJSON(filepath.Join(root, p.stateFile("run.json")), state); err != nil {
		return receipt, "", err
	}
	completed = true
	return receipt, dir, nil
}
func hashBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func Markdown(r Receipt) string {
	var b strings.Builder
	fmt.Fprint(&b, "# Proof-Driven Development receipt\n\n")
	name := r.PlanID
	if name == "" {
		name = "default"
	}
	fmt.Fprintf(&b, "Success: **%t**\n\nPlan: **%s**\n\nPlan digest: `%s`\n\nSource: `%s`\n\n", r.Success, name, r.PlanDigest, r.SourceDigest)
	fmt.Fprintf(&b, "Reviewer assertion: **%s** at %s\n\n", r.Approval.Reviewer, r.Approval.ApprovedAt.Format(time.RFC3339))
	if r.GitCommit != "" {
		fmt.Fprintf(&b, "Commit: `%s`\n\n", r.GitCommit)
	}
	if r.Inputs != nil {
		fmt.Fprintf(&b, "Input scope: %d files, %d directories, %d bytes; algorithm `%s`. [Manifest](%s)\n\n", r.Inputs.Files, r.Inputs.Directories, r.Inputs.Bytes, r.Inputs.Algorithm, r.Inputs.Manifest)
		policy, _ := json.Marshal(r.Inputs.Policy)
		fmt.Fprintf(&b, "Reviewed effective input policy: `%s`\n\n", policy)
	}
	fmt.Fprintln(&b, "| Requirement | Verification | Result |\n| --- | --- | --- |")
	for _, v := range r.Requirements {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", v.ID, v.Kind, v.Status)
	}
	fmt.Fprint(&b, "\n## Executed checks\n\n")
	fmt.Fprintln(&b, "| Check | Kind | Status | Duration | Dependencies | Log |\n| --- | --- | --- | --- | --- | --- |")
	for _, e := range r.Evidence {
		fmt.Fprintf(&b, "| %s | %s | %s | %d ms | %s | [%s](%s) |\n", e.Check, e.Kind, e.Execution.Status, e.Execution.DurationMS, strings.Join(e.Dependencies, ", "), e.Log, e.Log)
	}
	for _, e := range r.Evidence {
		for _, a := range e.Artifacts {
			fmt.Fprintf(&b, "\n- %s browser evidence: [%s](%s)\n", e.Check, a.File, a.File)
		}
	}
	fmt.Fprint(&b, "\n## Trusted boundary\n\n")
	for _, t := range r.Trust {
		fmt.Fprintf(&b, "- %s: %s (empirical check: %s)\n", t.ID, t.Assumption, t.Check)
	}
	fmt.Fprint(&b, "\n## Mutation checks\n\n")
	for _, m := range r.Mutations {
		fmt.Fprintf(&b, "- %s: %s ([log](%s))\n", m.ID, m.Status, m.Log)
	}
	fmt.Fprint(&b, "\n## Tool identities\n\n")
	var toolNames []string
	for name := range r.Tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	for _, name := range toolNames {
		fmt.Fprintf(&b, "- %s: `%s`\n", name, r.Tools[name])
	}
	fmt.Fprint(&b, "\n## Limits\n\n")
	for _, n := range r.NonGoals {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	fmt.Fprintln(&b, "\nFormal results cover stated contracts under the verifier's semantics and trusted assumptions. Empirical results cover executed test cases. Mutant rejection is a sensitivity check, not proof that a specification is complete.")
	if len(r.Gaps) > 0 {
		fmt.Fprint(&b, "\n## Gaps\n\n")
		for _, g := range r.Gaps {
			fmt.Fprintf(&b, "- %s\n", g)
		}
	}
	return b.String()
}

// Status rejects stale source/plan and damaged log artifacts, even when an old
// receipt was green. Receipt files are local attestations, not signed proofs.
func Status(root string, p Plan) (Receipt, string, error) { return StatusWithPolicy(root, p, "") }
func StatusWithPolicy(root string, p Plan, policyPath string) (Receipt, string, error) {
	return StatusContext(context.Background(), root, p, policyPath)
}
func StatusContext(ctx context.Context, root string, p Plan, policyPath string) (Receipt, string, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, "", err
	}
	unlock, err := lockProject(root)
	if err != nil {
		return Receipt{}, "", err
	}
	defer unlock()
	var receipt Receipt
	var dir string
	state, stateErr := readRunState(root, p)
	if stateErr == nil {
		dir = filepath.Join(root, p.stateFile("receipts"), state.ID)
		if state.State != "completed" {
			return receipt, dir, fmt.Errorf("latest verification is incomplete or interrupted (%s): %s", state.State, state.Detail)
		}
	} else if !os.IsNotExist(stateErr) {
		return receipt, "", stateErr
	} else {
		// Legacy receipts have no latest-run record. An unfinished newest
		// directory is still not permission to return an older green result.
		entries, err := os.ReadDir(filepath.Join(root, p.stateFile("receipts")))
		if err != nil {
			return receipt, "", fmt.Errorf("no receipt: %w", err)
		}
		var dirs []string
		for _, entry := range entries {
			if entry.IsDir() {
				dirs = append(dirs, entry.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
		if len(dirs) == 0 {
			return receipt, "", fmt.Errorf("no completed receipt")
		}
		dir = filepath.Join(root, p.stateFile("receipts"), dirs[0])
	}
	if err := NoSymlinks(dir); err != nil {
		return receipt, dir, err
	}
	if err = ReadJSON(filepath.Join(dir, "receipt.json"), &receipt); err != nil {
		return receipt, dir, err
	}
	if err = CheckApproval(root, p); err != nil {
		return receipt, dir, err
	}
	var currentApproval Approval
	if err = ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &currentApproval); err != nil || !sameApproval(receipt.Approval, currentApproval) {
		return receipt, dir, fmt.Errorf("stale receipt: approval record changed")
	}
	if (receipt.ApprovalPolicy != nil || currentApproval.Signature != nil) && policyPath == "" {
		return receipt, dir, fmt.Errorf("receipt requires --policy to revalidate signing authority and expiry")
	}
	if policyPath != "" {
		evidence, err := verifyPolicy(policyPath, p, currentApproval, time.Now().UTC())
		if err != nil {
			return receipt, dir, err
		}
		if receipt.ApprovalPolicy == nil || evidence != *receipt.ApprovalPolicy {
			return receipt, dir, fmt.Errorf("receipt was not verified under the current signing policy; prove again with --policy")
		}
	}
	if stateErr == nil && (!receipt.StartedAt.Equal(state.StartedAt) || !receipt.FinishedAt.Equal(state.FinishedAt)) {
		return receipt, dir, fmt.Errorf("latest-run record does not match its receipt")
	}
	if receipt.Schema != 1 || !matchesPlanID(receipt.PlanID, p) || receipt.FinishedAt.IsZero() {
		return receipt, dir, fmt.Errorf("invalid or unfinished receipt")
	}
	if len(receipt.Evidence) != len(p.Checks) || len(receipt.Requirements) != len(p.Requirements) || len(receipt.Mutations) != len(p.Mutations) {
		return receipt, dir, fmt.Errorf("receipt is missing planned evidence")
	}
	trustJSON, _ := json.Marshal(receipt.Trust)
	wantTrustJSON, _ := json.Marshal(p.Trust)
	limitsJSON, _ := json.Marshal(receipt.NonGoals)
	wantLimitsJSON, _ := json.Marshal(p.NonGoals)
	if !bytes.Equal(trustJSON, wantTrustJSON) || !bytes.Equal(limitsJSON, wantLimitsJSON) {
		return receipt, dir, fmt.Errorf("receipt trust or limitations differ from reviewed plan")
	}
	if receipt.Success {
		if len(receipt.Gaps) != 0 {
			return receipt, dir, fmt.Errorf("receipt success contradicts its gaps")
		}
		graph, err := resolvePackages(root, p)
		if err != nil {
			return receipt, dir, err
		}
		evidenceByID := map[string]Evidence{}
		for _, e := range receipt.Evidence {
			if _, duplicate := evidenceByID[e.Check]; duplicate {
				return receipt, dir, fmt.Errorf("receipt repeats a check")
			}
			evidenceByID[e.Check] = e
		}
		for _, c := range p.Checks {
			e, ok := evidenceByID[c.ID]
			deps := append([]string{}, e.Dependencies...)
			sort.Strings(deps)
			kind := "formal"
			if isEmpirical(c) {
				kind = "empirical"
			}
			if !ok || e.Kind != kind || e.Execution.Status != "passed" || e.Execution.ExitCode != 0 || strings.Join(deps, ",") != strings.Join(graph.Dependencies[c.ID], ",") {
				return receipt, dir, fmt.Errorf("receipt lacks a passing check or its verified dependencies")
			}
		}
		for i, m := range receipt.Mutations {
			if m.ID != p.Mutations[i].ID || m.Requirement != p.Mutations[i].Requirement || m.Status != "killed" || m.Execution.Status != "failed" || m.Execution.ExitCode <= 0 {
				return receipt, dir, fmt.Errorf("receipt lacks a killed mutation")
			}
		}
		for i, v := range receipt.Requirements {
			want := "evidenced"
			if p.Requirements[i].Kind == "formal" {
				want = "proved"
			}
			if v.ID != p.Requirements[i].ID || v.Kind != p.Requirements[i].Kind || v.Check != p.Requirements[i].Check || v.Status != want {
				return receipt, dir, fmt.Errorf("receipt has an unsupported verdict")
			}
		}
	}
	digest, err := Digest(root, p)
	if err != nil {
		return receipt, dir, err
	}
	snapshot, err := CaptureContext(ctx, root, p.Inputs)
	if err != nil {
		return receipt, dir, err
	}
	if digest != receipt.PlanDigest || snapshot.Hash != receipt.SourceDigest {
		return receipt, dir, fmt.Errorf("stale receipt: plan, contracts or source changed")
	}
	if receipt.Inputs == nil {
		return receipt, dir, fmt.Errorf("receipt lacks input provenance; run prove again")
	}
	if receipt.Inputs != nil {
		expected := snapshot.Record()
		expected.ManifestSHA256 = receipt.Inputs.ManifestSHA256
		expectedJSON, _ := json.Marshal(expected)
		recordedJSON, _ := json.Marshal(receipt.Inputs)
		if !bytes.Equal(expectedJSON, recordedJSON) {
			return receipt, dir, fmt.Errorf("input summary does not match current reviewed scope")
		}
		if receipt.Inputs.Algorithm != snapshot.Manifest.Algorithm || receipt.Inputs.Manifest != "inputs.json" {
			return receipt, dir, fmt.Errorf("invalid input manifest reference")
		}
		manifestBytes, err := readArtifact(filepath.Join(dir, "inputs.json"), 8<<20)
		if err != nil || hashBytes(manifestBytes) != receipt.Inputs.ManifestSHA256 {
			return receipt, dir, fmt.Errorf("missing or altered input manifest")
		}
		var manifest InputManifest
		if err = ReadJSON(filepath.Join(dir, "inputs.json"), &manifest); err != nil {
			return receipt, dir, err
		}
		encoded, _ := json.Marshal(manifest)
		if hashBytes(encoded) != receipt.SourceDigest {
			return receipt, dir, fmt.Errorf("input manifest does not match source digest")
		}
	}
	checkLog := func(name, hash string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if name == "" || filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid log reference")
		}
		data, err := readArtifact(filepath.Join(dir, name), (8<<20)+1024)
		if err != nil || hashBytes(data) != hash {
			return nil, fmt.Errorf("missing or altered log %s", name)
		}
		return data, nil
	}
	checks := map[string]Check{}
	for _, c := range p.Checks {
		checks[c.ID] = c
	}
	for _, e := range receipt.Evidence {
		data, err := checkLog(e.Log, e.LogSHA256)
		if err != nil {
			return receipt, dir, err
		}
		c, planned := checks[e.Check]
		if !planned || len(e.Artifacts) > browserArtifactCount {
			return receipt, dir, fmt.Errorf("receipt has unexpected check or artifacts")
		}
		var browserReport []byte
		seen := map[string]bool{}
		for _, artifact := range e.Artifacts {
			if !validBrowserArtifactName(c, artifact.File) || seen[artifact.File] {
				return receipt, dir, fmt.Errorf("invalid browser artifact %s", artifact.File)
			}
			seen[artifact.File] = true
			attachment, err := checkLog(artifact.File, artifact.SHA256)
			if err != nil || len(attachment) > browserArtifactLimit {
				return receipt, dir, fmt.Errorf("missing or altered browser artifact %s", artifact.File)
			}
			if artifact.File == browserReportFile(c) {
				browserReport = attachment
			}
		}
		if receipt.Success || e.Execution.Status == "passed" {
			if c.Kind == "playwright" {
				if e.Execution.ExitCode != 0 {
					return receipt, dir, fmt.Errorf("Playwright passed despite nonzero exit")
				}
				err = validateBrowserReport(browserReport, c, "")
			} else {
				err = validateExecutionOutput(c, recordedStdout(string(data)), string(data))
			}
			if err != nil {
				return receipt, dir, fmt.Errorf("saved check %s does not support success: %w", e.Check, err)
			}
		}
	}
	requirements := map[string]Requirement{}
	for _, r := range p.Requirements {
		requirements[r.ID] = r
	}
	for i, m := range receipt.Mutations {
		if m.Log == "" && m.Status != "killed" {
			continue
		}
		data, err := checkLog(m.Log, m.LogSHA256)
		if err != nil {
			return receipt, dir, err
		}
		if receipt.Success {
			execution := m.Execution
			execution.Output = string(data)
			if !RejectedMutation(execution, requirements[p.Mutations[i].Requirement], snapshot.Files[p.Mutations[i].File]) {
				return receipt, dir, fmt.Errorf("saved mutation %s does not reject its target postcondition", m.ID)
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return receipt, dir, err
	}
	// Authorization is independent of the project lock. Recheck after hashing
	// inputs/logs so expiry or an operator policy update during status fails closed.
	if policyPath != "" {
		evidence, err := verifyPolicy(policyPath, p, currentApproval, time.Now().UTC())
		if err != nil {
			return receipt, dir, err
		}
		if receipt.ApprovalPolicy == nil || evidence != *receipt.ApprovalPolicy {
			return receipt, dir, fmt.Errorf("review policy changed while checking status")
		}
	}
	return receipt, dir, nil
}

func sameApproval(a, b Approval) bool {
	if a.PlanID != b.PlanID || a.Schema != b.Schema || a.Digest != b.Digest || a.Reviewer != b.Reviewer || !a.ApprovedAt.Equal(b.ApprovedAt) {
		return false
	}
	leftSig, _ := json.Marshal(a.Signature)
	rightSig, _ := json.Marshal(b.Signature)
	if !bytes.Equal(leftSig, rightSig) {
		return false
	}
	aa, bb := append([]string{}, a.AcceptedTrust...), append([]string{}, b.AcceptedTrust...)
	sort.Strings(aa)
	sort.Strings(bb)
	left, _ := json.Marshal(a.Review)
	right, _ := json.Marshal(b.Review)
	return strings.Join(aa, ",") == strings.Join(bb, ",") && bytes.Equal(left, right)
}
