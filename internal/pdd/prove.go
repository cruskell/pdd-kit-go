package pdd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Evidence struct {
	Check     string    `json:"check"`
	Kind      string    `json:"kind"` // formal or empirical
	Execution Execution `json:"execution"`
	Log       string    `json:"log"`
	LogSHA256 string    `json:"log_sha256"`
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
	Schema       int               `json:"schema"`
	Version      string            `json:"version"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at"`
	PlanDigest   string            `json:"plan_digest"`
	SourceDigest string            `json:"source_digest"`
	GitCommit    string            `json:"git_commit,omitempty"`
	Approval     Approval          `json:"approval"`
	Tools        map[string]string `json:"tools"`
	Success      bool              `json:"success"`
	Evidence     []Evidence        `json:"evidence"`
	Mutations    []MutationResult  `json:"mutations"`
	Requirements []Verdict         `json:"requirements"`
	Trust        []Trust           `json:"trusted_boundary"`
	NonGoals     []string          `json:"non_goals"`
	Gaps         []string          `json:"gaps"`
}
type Options struct {
	Version  string
	Runner   Runner
	Tools    map[string]string
	Progress func(string)
}

// Prove executes only approved checks. Missing tools, missing tests, surviving
// mutants and changes during verification all fail closed and produce a receipt.
func Prove(ctx context.Context, root string, p Plan, options Options) (Receipt, string, error) {
	receipt := Receipt{Schema: 1, Version: options.Version, StartedAt: time.Now().UTC(), Evidence: []Evidence{}, Mutations: []MutationResult{}, Requirements: []Verdict{}, Trust: p.Trust, NonGoals: p.NonGoals, Gaps: []string{}, Tools: options.Tools}
	if err := p.Validate(root); err != nil {
		return receipt, "", err
	}
	if err := CheckApproval(root, p); err != nil {
		return receipt, "", err
	}
	if err := ReadJSON(filepath.Join(root, ApprovalPath), &receipt.Approval); err != nil {
		return receipt, "", err
	}
	digest, err := Digest(root, p)
	if err != nil {
		return receipt, "", err
	}
	receipt.PlanDigest = digest
	source, err := Capture(root)
	if err != nil {
		return receipt, "", err
	}
	receipt.SourceDigest = source.Hash
	if b, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output(); err == nil {
		receipt.GitCommit = strings.TrimSpace(string(b))
	}
	// In-process callers cannot claim proof without a real backend. Tests inject a
	// runner to exercise orchestration; only ToolRunner is exposed by the CLI.
	runner := options.Runner
	if runner == nil {
		runner = ToolRunner{}
	}
	dir := filepath.Join(root, ".pdd", "receipts", receipt.StartedAt.Format("20060102T150405.000000000Z"))
	if err = NoSymlinks(dir); err != nil {
		return receipt, "", err
	}
	if err = os.MkdirAll(dir, 0755); err != nil {
		return receipt, "", err
	}
	scratch, err := os.MkdirTemp("", "pdd-baseline-")
	if err != nil {
		return receipt, "", err
	}
	defer os.RemoveAll(scratch)
	if err = source.Materialize(scratch); err != nil {
		return receipt, "", err
	}
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
	for _, c := range p.Checks {
		checks[c.ID] = c
		progress("Checking " + c.ID + " (" + c.Kind + ")")
		if err = os.RemoveAll(scratch); err != nil {
			return receipt, "", err
		}
		if err = os.MkdirAll(scratch, 0755); err != nil {
			return receipt, "", err
		}
		if err = source.Materialize(scratch); err != nil {
			return receipt, "", err
		}
		e := runner.Run(ctx, scratch, c)
		after, captureErr := Capture(scratch)
		if captureErr != nil || after.Hash != source.Hash {
			e.Status = "failed"
			e.Detail = "check modified its source snapshot"
		}
		executions[c.ID] = e
		log, hash, err := saveLog(c.ID, e)
		if err != nil {
			return receipt, "", err
		}
		kind := "empirical"
		if c.Kind == "gobra" {
			kind = "formal"
		}
		receipt.Evidence = append(receipt.Evidence, Evidence{c.ID, kind, e, log, hash})
		if e.Status != "passed" {
			receipt.Gaps = append(receipt.Gaps, c.ID+": "+e.Status+" "+e.Detail)
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
		if executions[m.Check].Status == "passed" {
			progress("Mutating " + m.ID)
			temp, err := os.MkdirTemp("", "pdd-mutant-")
			if err != nil {
				return receipt, "", err
			}
			err = source.Materialize(temp)
			if err == nil {
				var b []byte
				b, err = Mutate(m, reqs[m.Requirement], source.Files[m.File])
				if err == nil {
					err = os.WriteFile(filepath.Join(temp, filepath.FromSlash(m.File)), b, 0644)
				}
			}
			if err != nil {
				os.RemoveAll(temp)
				return receipt, "", err
			}
			mr.Execution = runner.Run(ctx, temp, checks[m.Check])
			os.RemoveAll(temp)
			mr.Status = "inconclusive"
			if mr.Execution.Status == "passed" {
				mr.Status = "survived"
			} else if RejectedMutation(mr.Execution, reqs[m.Requirement].Ensures) {
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
	for _, r := range p.Requirements {
		v := Verdict{r.ID, r.Kind, "unproven", r.Check}
		if executions[r.Check].Status == "passed" {
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
	current, err := Capture(root)
	if err != nil || current.Hash != source.Hash {
		receipt.Gaps = append(receipt.Gaps, "source changed during verification")
	}
	latest, err := Load(root)
	if err != nil {
		receipt.Gaps = append(receipt.Gaps, "plan changed during verification")
	} else if d, e := Digest(root, latest); e != nil || d != digest {
		receipt.Gaps = append(receipt.Gaps, "plan or contracts changed during verification")
	}
	var finalApproval Approval
	approvalErr := ReadJSON(filepath.Join(root, ApprovalPath), &finalApproval)
	if CheckApproval(root, p) != nil || approvalErr != nil || !sameApproval(receipt.Approval, finalApproval) {
		receipt.Gaps = append(receipt.Gaps, "approval changed during verification")
	}
	receipt.Success = len(receipt.Gaps) == 0
	receipt.FinishedAt = time.Now().UTC()
	if !receipt.Success {
		for i := range receipt.Requirements {
			if strings.Contains(strings.Join(receipt.Gaps, " "), "changed during verification") {
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
	return receipt, dir, nil
}
func hashBytes(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func Markdown(r Receipt) string {
	var b strings.Builder
	fmt.Fprint(&b, "# Proof-Driven Development receipt\n\n")
	fmt.Fprintf(&b, "Success: **%t**\n\nPlan: `%s`\n\nSource: `%s`\n\n", r.Success, r.PlanDigest, r.SourceDigest)
	fmt.Fprintln(&b, "| Requirement | Verification | Result |\n| --- | --- | --- |")
	for _, v := range r.Requirements {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", v.ID, v.Kind, v.Status)
	}
	fmt.Fprint(&b, "\n## Trusted boundary\n\n")
	for _, t := range r.Trust {
		fmt.Fprintf(&b, "- %s: %s (empirical check: %s)\n", t.ID, t.Assumption, t.Check)
	}
	fmt.Fprint(&b, "\n## Mutation checks\n\n")
	for _, m := range r.Mutations {
		fmt.Fprintf(&b, "- %s: %s\n", m.ID, m.Status)
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
func Status(root string, p Plan) (Receipt, string, error) {
	var receipt Receipt
	entries, err := os.ReadDir(filepath.Join(root, ".pdd", "receipts"))
	if err != nil {
		return receipt, "", fmt.Errorf("no receipt: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	var dir string
	for _, d := range dirs {
		candidate := filepath.Join(root, ".pdd", "receipts", d)
		if _, err := os.Stat(filepath.Join(candidate, "receipt.json")); err == nil {
			dir = candidate
			break
		}
	}
	if dir == "" {
		return receipt, "", fmt.Errorf("no completed receipt")
	}
	if err = ReadJSON(filepath.Join(dir, "receipt.json"), &receipt); err != nil {
		return receipt, dir, err
	}
	if err = CheckApproval(root, p); err != nil {
		return receipt, dir, err
	}
	var currentApproval Approval
	if err = ReadJSON(filepath.Join(root, ApprovalPath), &currentApproval); err != nil || !sameApproval(receipt.Approval, currentApproval) {
		return receipt, dir, fmt.Errorf("stale receipt: approval record changed")
	}
	if receipt.Schema != 1 || receipt.FinishedAt.IsZero() {
		return receipt, dir, fmt.Errorf("invalid or unfinished receipt")
	}
	if len(receipt.Evidence) != len(p.Checks) || len(receipt.Requirements) != len(p.Requirements) || len(receipt.Mutations) != len(p.Mutations) {
		return receipt, dir, fmt.Errorf("receipt is missing planned evidence")
	}
	if receipt.Success {
		if len(receipt.Gaps) != 0 {
			return receipt, dir, fmt.Errorf("receipt success contradicts its gaps")
		}
		for i, e := range receipt.Evidence {
			if e.Check != p.Checks[i].ID || e.Execution.Status != "passed" {
				return receipt, dir, fmt.Errorf("receipt lacks a passing check")
			}
		}
		for i, m := range receipt.Mutations {
			if m.ID != p.Mutations[i].ID || m.Status != "killed" {
				return receipt, dir, fmt.Errorf("receipt lacks a killed mutation")
			}
		}
		for i, v := range receipt.Requirements {
			want := "evidenced"
			if p.Requirements[i].Kind == "formal" {
				want = "proved"
			}
			if v.ID != p.Requirements[i].ID || v.Kind != p.Requirements[i].Kind || v.Status != want {
				return receipt, dir, fmt.Errorf("receipt has an unsupported verdict")
			}
		}
	}
	digest, err := Digest(root, p)
	if err != nil {
		return receipt, dir, err
	}
	snapshot, err := Capture(root)
	if err != nil {
		return receipt, dir, err
	}
	if digest != receipt.PlanDigest || snapshot.Hash != receipt.SourceDigest {
		return receipt, dir, fmt.Errorf("stale receipt: plan, contracts or source changed")
	}
	checkLog := func(name, hash string) error {
		if name == "" || filepath.Base(name) != name {
			return fmt.Errorf("invalid log reference")
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || hashBytes(b) != hash {
			return fmt.Errorf("missing or altered log %s", name)
		}
		return nil
	}
	for _, e := range receipt.Evidence {
		if err = checkLog(e.Log, e.LogSHA256); err != nil {
			return receipt, dir, err
		}
	}
	for _, m := range receipt.Mutations {
		if m.Log != "" {
			if err = checkLog(m.Log, m.LogSHA256); err != nil {
				return receipt, dir, err
			}
		}
	}
	return receipt, dir, nil
}

func sameApproval(a, b Approval) bool {
	if a.Schema != b.Schema || a.Digest != b.Digest || a.Reviewer != b.Reviewer || !a.ApprovedAt.Equal(b.ApprovedAt) {
		return false
	}
	aa, bb := append([]string{}, a.AcceptedTrust...), append([]string{}, b.AcceptedTrust...)
	sort.Strings(aa)
	sort.Strings(bb)
	return strings.Join(aa, ",") == strings.Join(bb, ",")
}
