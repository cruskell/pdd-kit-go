package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cruskell/proofsteer-go/internal/pdd"
)

func workflowCommand(c string) bool {
	switch c {
	case "draft", "decisions", "decide", "review", "tasks", "build", "report", "pr":
		return true
	}
	return false
}
func runWorkflow(root string, g globals, args []string, printer output, out, diagnostics io.Writer, version string) int {
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(diagnostics)
	request := f.String("request-file", "", "Original request text file")
	driver := f.String("driver", "codex", "Agent driver")
	worker := f.String("worker", "", "JSON worker executable")
	files := f.String("files", "", "Comma-separated source files")
	agentTimeout := f.Duration("agent-timeout", 5*time.Minute, "Agent timeout")
	expected := f.String("expect-draft", "", "Inspected draft hash")
	id := f.String("id", "", "Decision ID")
	option := f.String("option", "", "Option ID")
	reason := f.String("reason", "", "Decision reason")
	role := f.String("role", "both", "Review role")
	apply := f.Bool("apply", false, "Install candidate")
	iterations := f.Int("max-iterations", 3, "Maximum repairs")
	goExe := f.String("go", "go", "Go executable")
	jar := f.String("gobra-jar", "", "Gobra jar")
	timeout := f.Duration("timeout", 2*time.Minute, "Check timeout")
	destination := f.String("out", "", "Output file")
	title := f.String("title", "", "PR title")
	base := f.String("base", "main", "PR base")
	head := f.String("head", "", "PR head")
	create := f.Bool("create", false, "Create draft PR")
	fail := func(err error) int { return printer.finish(1, nil, "workflow", err, "") }
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printer.finish(0, map[string]string{"help": help}, "", nil, "")
		}
		return printer.finish(2, nil, "usage", err, "")
	}
	allowed := map[string]string{
		"draft":  "request-file driver worker files agent-timeout expect-draft apply",
		"review": "role driver worker files agent-timeout",
		"build":  "driver worker files agent-timeout apply max-iterations go gobra-jar timeout",
		"decide": "id option reason expect-draft", "report": "out", "pr": "title base head create", "decisions": "", "tasks": "",
	}
	var optionErr error
	f.Visit(func(v *flag.Flag) {
		if !strings.Contains(" "+allowed[args[0]]+" ", " "+v.Name+" ") {
			optionErr = fmt.Errorf("--%s is not valid for %s", v.Name, args[0])
		}
	})
	if optionErr != nil {
		return printer.finish(2, nil, "usage", optionErr, "")
	}
	if f.NArg() != 0 {
		return fail(fmt.Errorf("unexpected positional arguments"))
	}
	resolve := func(p string) string {
		if p != "" && !filepath.IsAbs(p) {
			return filepath.Join(root, p)
		}
		return p
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	opts := pdd.AgentOptions{Driver: *driver, PolicyPath: resolve(g.policy), Timeout: *agentTimeout}
	if *worker != "" {
		path := *worker
		if strings.ContainsAny(path, "/\\") {
			path = resolve(path)
		}
		opts.Worker = []string{path}
	}
	if *files != "" {
		opts.Files = strings.Split(*files, ",")
	}
	if *timeout <= 0 || *agentTimeout <= 0 {
		return fail(fmt.Errorf("timeouts must be positive"))
	}
	emit := func(data interface{}) int {
		if !g.json {
			b, e := json.MarshalIndent(data, "", "  ")
			if e != nil {
				return fail(e)
			}
			fmt.Fprintln(out, string(b))
		}
		return printer.finish(0, data, "", nil, "")
	}
	switch args[0] {
	case "draft":
		if *request == "" {
			return fail(fmt.Errorf("--request-file is required"))
		}
		text, e := os.ReadFile(resolve(*request))
		if e != nil {
			return fail(e)
		}
		result, e := pdd.GenerateDraft(ctx, root, g.plan, string(text), *expected, *apply, opts)
		if e != nil {
			return fail(e)
		}
		return emit(result)
	case "decisions":
		view, e := pdd.InspectDraft(root, g.plan)
		if e != nil {
			return fail(e)
		}
		return emit(view)
	case "decide":
		if *expected == "" {
			return fail(fmt.Errorf("--expect-draft is required; inspect decisions first"))
		}
		result, e := pdd.AnswerDecision(root, g.plan, *id, *option, *reason, *expected)
		if e != nil {
			return fail(e)
		}
		return emit(result)
	case "report":
		if *destination == "" {
			return fail(fmt.Errorf("--out is required"))
		}
		if e := pdd.WriteDashboard(ctx, root, g.plan, opts.PolicyPath, resolve(*destination)); e != nil {
			return fail(e)
		}
		return emit(map[string]string{"path": resolve(*destination)})
	}
	plan, e := pdd.LoadNamed(root, g.plan)
	if e != nil {
		return fail(e)
	}
	switch args[0] {
	case "review":
		roles := []string{*role}
		if *role == "both" {
			roles = []string{"intent", "guarantees"}
		}
		results := []pdd.AnalysisRecord{}
		ready := true
		for _, r := range roles {
			plan, e = pdd.LoadNamed(root, g.plan)
			if e != nil {
				return fail(e)
			}
			a, e := pdd.Analyze(ctx, root, plan, r, opts)
			if e != nil {
				return fail(e)
			}
			results = append(results, a)
			ready = ready && a.Result.Status == "ready"
		}
		if !ready {
			return printer.finish(1, results, "review", fmt.Errorf("review findings remain open"), "")
		}
		return emit(results)
	case "tasks":
		view, e := pdd.WorkflowReport(ctx, root, plan, opts.PolicyPath)
		if e != nil {
			return fail(e)
		}
		return emit(view)
	case "build":
		result, e := pdd.Build(ctx, root, plan, pdd.BuildOptions{Agent: opts, Iterations: *iterations, Apply: *apply, Engine: pdd.Options{PolicyPath: opts.PolicyPath, Version: version, Runner: pdd.ToolRunner{Go: *goExe, GobraJar: resolve(*jar), Timeout: *timeout}, Tools: pdd.ToolInfo(ctx, *goExe, resolve(*jar)), Progress: func(s string) { fmt.Fprintln(diagnostics, s) }}})
		if e != nil {
			return printer.finish(1, result, "build", e, "")
		}
		if !result.Success {
			return printer.finish(1, result, "build", fmt.Errorf("%s: %s", result.Stage, result.Message), "")
		}
		return emit(result)
	case "pr":
		result, e := pdd.PreparePR(ctx, root, plan, opts.PolicyPath, pdd.PROptions{Title: *title, Base: *base, Head: *head, Create: *create})
		if e != nil {
			return fail(e)
		}
		return emit(result)
	}
	return fail(fmt.Errorf("unknown workflow command"))
}
