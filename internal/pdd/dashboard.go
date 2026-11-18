package pdd

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// WriteDashboard renders a portable, offline review surface. Template escaping
// applies to every repository- and model-supplied string; no script is emitted.
func WriteDashboard(ctx context.Context, root, name, policy, destination string) error {
	view, err := InspectDraft(root, name)
	if err != nil {
		return err
	}
	var workflow *WorkflowView
	if view.ValidationError == "" {
		w, e := WorkflowReport(ctx, root, view.Plan, policy)
		if e != nil {
			return e
		}
		workflow = &w
	}
	data := struct {
		Draft    PlanningView
		Workflow *WorkflowView
		JSON     string
		Review   *ReviewReport
		Evidence *dashboardEvidence
	}{Draft: view, Workflow: workflow}
	if view.ValidationError == "" {
		r, e := ReportReview(root, view.Plan)
		if e != nil {
			return e
		}
		data.Review = &r
	}
	if workflow != nil && workflow.receipt != nil {
		data.Evidence = evidenceForDashboard(view.Plan, *workflow.receipt, workflow.receiptDir, destination)
	}
	raw, err := json.MarshalIndent(view.Plan, "", "  ")
	if err != nil {
		return err
	}
	data.JSON = string(raw)
	t, err := template.New("review").Parse(dashboardTemplate)
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err = t.Execute(&b, data); err != nil {
		return err
	}
	return AtomicWrite(destination, b.Bytes())
}

//go:embed dashboard.html
var dashboardTemplate string

type dashboardLink struct{ Label, URL string }

type dashboardCheck struct {
	Evidence
	Backend string
	LogURL  string
	Files   []dashboardLink
	Tests   []string
}

type dashboardMutation struct {
	MutationResult
	LogURL string
}

type dashboardEvidence struct {
	Receipt
	Verdicts         map[string]string
	Checks           []dashboardCheck
	NegativeExamples []dashboardMutation
	ReceiptURL       string
}

func evidenceForDashboard(plan Plan, r Receipt, dir, destination string) *dashboardEvidence {
	link := func(name string) string {
		if name == "" || filepath.Base(name) != name {
			return ""
		}
		rel, err := filepath.Rel(filepath.Dir(destination), filepath.Join(dir, name))
		if err != nil {
			return ""
		}
		return (&url.URL{Path: filepath.ToSlash(rel)}).String()
	}
	result := &dashboardEvidence{Receipt: r, ReceiptURL: link("receipt.json"), Verdicts: map[string]string{}}
	for _, v := range r.Requirements {
		result.Verdicts[v.ID] = v.Status
	}
	checks := map[string]Check{}
	for _, check := range plan.Checks {
		checks[check.ID] = check
	}
	for _, e := range r.Evidence {
		c := checks[e.Check]
		item := dashboardCheck{Evidence: e, Backend: c.Kind, LogURL: link(e.Log), Tests: c.Tests}
		for _, a := range e.Artifacts {
			label := a.File
			switch filepath.Ext(a.File) {
			case ".json":
				label = "Playwright JSON"
			case ".png":
				label = "Failure screenshot"
			case ".zip":
				label = "Playwright trace"
			}
			item.Files = append(item.Files, dashboardLink{Label: label, URL: link(a.File)})
		}
		result.Checks = append(result.Checks, item)
	}
	for _, m := range r.Mutations {
		result.NegativeExamples = append(result.NegativeExamples, dashboardMutation{MutationResult: m, LogURL: link(m.Log)})
	}
	return result
}

type PROptions struct {
	Title, Base, Head string
	Create            bool
}
type PRPreview struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Path  string `json:"path"`
	URL   string `json:"url,omitempty"`
}

func PreparePR(ctx context.Context, root string, p Plan, policy string, opts PROptions) (PRPreview, error) {
	view, err := WorkflowReport(ctx, root, p, policy)
	if err != nil {
		return PRPreview{}, err
	}
	if !view.ReadyForPR {
		return PRPreview{}, fmt.Errorf("workflow is not ready for a PR: %s", strings.Join(view.Gaps, "; "))
	}
	receipt, _, err := StatusContext(ctx, root, p, policy)
	if err != nil {
		return PRPreview{}, err
	}
	title := opts.Title
	if title == "" {
		title = p.Name
	}
	var body strings.Builder
	fmt.Fprintf(&body, "%s\n\n%s", p.Intent, Markdown(receipt))
	for _, trust := range p.Trust {
		for _, clause := range trust.Clauses {
			fmt.Fprintf(&body, "\nTrusted clause %s: %s. If false: %s. Empirical test: %s / %s.\n", clause.ID, clause.Text, clause.Impact, trust.Check, clause.Test)
		}
	}
	fmt.Fprintln(&body, "\nReceipt log links refer to saved local/CI artifacts; attach the corresponding CI artifacts for log inspection.")
	for _, r := range p.PlanningReviews {
		fmt.Fprintf(&body, "\nAdvisory %s review: %s — %s\n", r.Role, r.Result.Status, r.Result.Summary)
	}
	if view.GapReview != nil {
		fmt.Fprintf(&body, "\nAdvisory gap review: %s — %s\n", view.GapReview.Result.Status, view.GapReview.Result.Summary)
	}
	result := PRPreview{Title: title, Body: body.String(), Path: filepath.Join(root, p.stateFile("PR.md"))}
	if err = AtomicWrite(result.Path, []byte(result.Body)); err != nil {
		return result, err
	}
	if !opts.Create {
		return result, nil
	}
	git := func(args ...string) (string, error) {
		c := exec.CommandContext(ctx, "git", args...)
		c.Dir = root
		b, e := c.Output()
		return strings.TrimSpace(string(b)), e
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil || receipt.GitCommit == "" || head != receipt.GitCommit {
		return result, fmt.Errorf("current receipt must bind the committed HEAD before PR creation")
	}
	dirty, err := git("status", "--porcelain")
	if err != nil || dirty != "" {
		return result, fmt.Errorf("commit all changes and re-prove before PR creation")
	}
	branch, err := git("branch", "--show-current")
	if err != nil || branch == "" {
		return result, fmt.Errorf("PR creation requires a named branch")
	}
	if opts.Head != "" && opts.Head != branch {
		return result, fmt.Errorf("--head must match the verified current branch")
	}
	base := opts.Base
	if base == "" {
		base = "main"
	}
	if strings.HasPrefix(base, "-") || branch == base {
		return result, fmt.Errorf("select a feature branch and valid PR base")
	}
	remote, err := git("ls-remote", "--exit-code", "origin", "refs/heads/"+branch)
	if err != nil || !strings.HasPrefix(remote, head+"\t") {
		return result, fmt.Errorf("push the verified branch before creating a PR")
	}
	// Recheck immediately before invoking an external service. The command never
	// pushes, merges, bypasses branch protection or approves the plan.
	if _, _, err = StatusContext(ctx, root, p, policy); err != nil {
		return result, err
	}
	c := exec.CommandContext(ctx, "gh", "pr", "create", "--draft", "--title", title, "--body-file", result.Path, "--base", base, "--head", branch)
	c.Dir = root
	b, err := c.CombinedOutput()
	if err != nil {
		return result, fmt.Errorf("gh pr create: %w: %s", err, b)
	}
	result.URL = strings.TrimSpace(string(b))
	return result, nil
}
