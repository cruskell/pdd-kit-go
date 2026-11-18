package pdd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
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
	}{Draft: view, Workflow: workflow}
	if view.ValidationError == "" {
		r, e := ReportReview(root, view.Plan)
		if e != nil {
			return e
		}
		data.Review = &r
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

const dashboardTemplate = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>PDD Go review</title><style>body{font:16px system-ui;max-width:1100px;margin:40px auto;padding:0 20px;color:#172b42}article,section{padding:16px;border:1px solid #ccd6e0;border-radius:8px;margin:16px 0}h1,h2{color:#163c6a}table{border-collapse:collapse;width:100%}td,th{padding:10px;border:1px solid #ccd6e0;text-align:left}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#f1f5f9;padding:14px}small{overflow-wrap:anywhere} .warning{color:#9a3010}</style><h1>{{.Draft.Plan.Name}}</h1><p>{{.Draft.Plan.Intent}}</p><small>Plan {{.Draft.PlanID}} · Draft {{.Draft.Digest}}</small><p>Human decisions → independent intent and guarantee reviews → human approval → implementation → machine verification → advisory gap review → PR</p>{{if .Draft.ValidationError}}<p class="warning">Before approval: {{.Draft.ValidationError}}</p>{{end}}
<h2>Decision cards</h2>{{range .Draft.Plan.Decisions}}<article><h3>{{.Question}}</h3><p>{{.Context}}</p><small>{{.File}} {{.Function}} · {{.ID}}</small>{{range .Options}}<p><b>{{.ID}}: {{.Label}}</b>{{if .Recommended}} (recommended){{end}} — {{.Consequence}}</p>{{end}}<p>Selected: {{.Selected}} · Reason: {{.Rationale}}</p></article>{{else}}<p>No decision cards recorded.</p>{{end}}
<h2>Original intent → specification landscape</h2>{{range .Draft.Plan.IntentMap}}<section><blockquote>{{.UserWords}}</blockquote><p>Requirements: {{range .Requirements}}{{.}} {{end}}</p><p>Decisions: {{range .Decisions}}{{.}} {{end}}</p></section>{{end}}<table><tr><th>Requirement</th><th>Guarantee</th><th>Evidence</th></tr>{{range .Draft.Plan.Requirements}}<tr><td>{{.ID}}</td><td>{{.Text}}<pre>{{.Ensures}}</pre></td><td>{{.Kind}} · {{.Check}}<br>{{.File}} {{.Function}}</td></tr>{{end}}</table>
{{with .Review}}<h2>Frozen approval contract</h2><p>Approval state: {{.Approval.State}} — {{.Approval.Message}}</p><small>Plan digest for explicit approval: {{.Digest}}</small>{{range .Bundle.Sources}}<section><h3>{{.File}}</h3>{{range .Annotations}}<pre>{{.}}</pre>{{end}}{{if .Definitions}}<details><summary>Pure specification definitions</summary><pre>{{printf "%s" .Definitions}}</pre></details>{{end}}{{range .Bindings}}<p><small>{{.Owner}}</small> · {{.Annotation}}</p>{{end}}</section>{{end}}{{end}}
<h2>Trusted boundary and assumed clauses</h2>{{range .Draft.Plan.Trust}}<article><h3>{{.ID}}</h3><p>{{.Assumption}}</p><p>Check: {{.Check}} · Affects: {{range .Affects}}{{.}} {{end}}</p>{{range .Clauses}}<p><b>{{.ID}}</b>: {{.Text}} · If false: {{.Impact}} · Test {{.Test}}</p>{{end}}</article>{{end}}
<h2>Given / When / Then tests</h2>{{range .Draft.Plan.Scenarios}}<section><h3>{{.ID}} · {{.Purpose}}</h3><p>Given {{.Given}}<br>When {{.When}}<br>Then {{.Then}}</p><small>{{.Check}} / {{.Test}}</small></section>{{end}}
<h2>Independent advisory reviews</h2>{{range .Draft.Plan.PlanningReviews}}<section><b>{{.Role}}: {{.Result.Status}}</b><p>{{.Result.Summary}}</p>{{range .Result.Findings}}<p>{{.Severity}}: {{.Text}}</p>{{end}}<small>Invocation {{.Invocation}} · {{.Driver}}</small></section>{{end}}<p>Model opinions are advisory; they do not establish formal correctness or authenticate a human reviewer.</p>
{{with .Workflow}}<h2>Verification task graph</h2><table><tr><th>Task</th><th>Depends on</th><th>Evidence status</th></tr>{{range .Tasks}}<tr><td>{{.ID}} ({{.Kind}})</td><td>{{range .DependsOn}}{{.}} {{end}}</td><td>{{.Status}}</td></tr>{{end}}</table><p>Machine receipt current: {{.MachineCurrent}} · successful: {{.MachineSuccess}} · ready for PR: {{.ReadyForPR}}</p>{{range .Gaps}}<p class="warning">{{.}}</p>{{end}}{{with .GapReview}}<p>Advisory gap review: {{.Result.Status}} — {{.Result.Summary}}</p>{{end}}<p>Potential redundant formal regression scenarios (manual review only): {{range .PruneCandidates}}{{.}} {{end}}</p>{{end}}
<h2>Limits</h2>{{range .Draft.Plan.NonGoals}}<p>{{.}}</p>{{end}}<details><summary>Complete plan</summary><pre>{{.JSON}}</pre></details></html>`

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
