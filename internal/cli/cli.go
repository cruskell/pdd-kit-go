package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/cruskell/pdd-kit-go/internal/pdd"
)

const help = `pdd — Proof-Driven Development for Go

Usage: pdd [--cwd DIR] <command> [options]

  init       Create an unapproved .pdd/plan.json scaffold
  plan       Review intent, clauses, checks, mutations and trusted boundaries
  lint       Validate the plan and its production Go contract mappings
  approve    Record review: --reviewer NAME [--accept-tcb id1,id2]
  prove      Run Gobra proofs, named Go tests and negative mutations
  status     Check whether the latest receipt is successful and current
  doctor     Check local Go and Docker/Java prerequisites

Global: --cwd DIR, --version, --help
prove: --go PATH --gobra-jar PATH --timeout 2m
       Default Gobra backend: pinned Docker image; missing tools fail closed.
       This executes project tests in scratch copies, without OS sandboxing.
approve: --reviewer NAME --accept-tcb ID[,ID...]
         Records an explicit local assertion; does not authenticate identity.

Start with init, or copy examples/login/plan.json into .pdd/plan.json
in this repository for a runnable demo. Edit and review the plan before approve.
`

func Run(args []string, out, errOut io.Writer, version string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--cwd" || a == "-C" {
			if i+1 >= len(args) {
				fmt.Fprintln(errOut, "--cwd requires a directory")
				return 2
			}
			i++
			cwd = args[i]
		} else if strings.HasPrefix(a, "--cwd=") {
			cwd = strings.TrimPrefix(a, "--cwd=")
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 || rest[0] == "--help" || rest[0] == "-h" || rest[0] == "help" {
		fmt.Fprint(out, help)
		return 0
	}
	if rest[0] == "--version" {
		fmt.Fprintln(out, version)
		return 0
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}
	st, err := os.Stat(cwd)
	if err != nil || !st.IsDir() {
		fmt.Fprintln(errOut, "--cwd must name an existing directory")
		return 2
	}
	fs := flag.NewFlagSet(rest[0], flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(out, help) }
	var reviewer, tcb, goExe, jar string
	var timeout time.Duration
	switch rest[0] {
	case "approve":
		fs.StringVar(&reviewer, "reviewer", "", "Reviewer assertion")
		fs.StringVar(&tcb, "accept-tcb", "", "Comma-separated trust ids")
	case "prove", "doctor":
		fs.StringVar(&goExe, "go", "go", "Go executable")
		fs.StringVar(&jar, "gobra-jar", "", "Local Gobra jar instead of Docker")
		fs.DurationVar(&timeout, "timeout", 2*time.Minute, "Timeout per check")
	case "init", "plan", "lint", "status":
	default:
		fmt.Fprintln(errOut, "unknown command:", rest[0])
		return 2
	}
	if err = fs.Parse(rest[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(errOut, "unexpected argument:", fs.Arg(0))
		return 2
	}
	fail := func(err error) int { fmt.Fprintln(errOut, "error:", err); return 1 }
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if rest[0] == "doctor" {
		good := true
		for name, command := range map[string][]string{"go": {goExe, "version"}, "gobra-container": {"docker", "image", "inspect", pdd.GobraImage, "--format", "{{.Id}}"}} {
			if name == "gobra-container" && jar != "" {
				if _, err := os.Stat(jar); err != nil {
					fmt.Fprintln(out, "gobra-jar:", err)
					good = false
				}
				command = []string{"java", "-version"}
			}
			probe, c := context.WithTimeout(ctx, 10*time.Second)
			b, err := exec.CommandContext(probe, command[0], command[1:]...).CombinedOutput()
			c()
			if err != nil {
				fmt.Fprintf(out, "%s: unavailable (%v)\n", name, err)
				good = false
			} else {
				fmt.Fprintf(out, "%s: %s\n", name, strings.TrimSpace(string(b)))
			}
		}
		if !good {
			fmt.Fprintf(out, "For Docker backend: docker pull %s\n", pdd.GobraImage)
			return 1
		}
		return 0
	}
	if rest[0] == "init" {
		path := filepath.Join(cwd, pdd.PlanPath)
		if err = pdd.NoSymlinks(path); err != nil {
			return fail(err)
		}
		if _, err = os.Lstat(path); !os.IsNotExist(err) {
			return fail(fmt.Errorf("%s already exists or cannot be accessed", path))
		}
		scaffold := pdd.Plan{Schema: 1, Name: filepath.Base(cwd), Intent: "Describe the behavior to prove, including refusal and success cases.", NonGoals: []string{"List behavior and external dependencies outside this proof."}, Requirements: []pdd.Requirement{}, Checks: []pdd.Check{}, Trust: []pdd.Trust{}, Mutations: []pdd.Mutation{}}
		if err = pdd.WriteJSON(path, scaffold); err != nil {
			return fail(err)
		}
		fmt.Fprintln(out, "Created", path, "(draft: add requirements, contracts and checks before approval)")
		return 0
	}
	plan, err := pdd.Load(cwd)
	if err != nil {
		return fail(err)
	}
	switch rest[0] {
	case "plan":
		fmt.Fprintf(out, "# %s\n\n%s\n\n", plan.Name, plan.Intent)
		for _, r := range plan.Requirements {
			fmt.Fprintf(out, "[%s] %s: %s\n  check: %s\n", r.Kind, r.ID, r.Text, r.Check)
			if r.Kind == "formal" {
				fmt.Fprintf(out, "  %s :: %s\n  ensures %s\n", r.File, r.Function, r.Ensures)
			}
		}
		fmt.Fprintln(out, "\nTrusted boundary (requires explicit acceptance):")
		for _, t := range plan.Trust {
			fmt.Fprintf(out, "  %s: %s\n  empirical check: %s\n", t.ID, t.Assumption, t.Check)
		}
		fmt.Fprintln(out, "\nOutside the guarantee:")
		for _, n := range plan.NonGoals {
			fmt.Fprintln(out, " -", n)
		}
		fmt.Fprintln(out, "\nMutations:")
		for _, m := range plan.Mutations {
			fmt.Fprintf(out, "  %s -> %s: %q => %q\n", m.ID, m.Requirement, m.Find, m.Replace)
		}
		if err = pdd.CheckApproval(cwd, plan); err != nil {
			fmt.Fprintln(out, "\nApproval: pending or stale")
		} else {
			fmt.Fprintln(out, "\nApproval: current")
		}
		return 0
	case "lint":
		fmt.Fprintln(out, "Plan, production contracts, trust mappings and mutations are structurally valid.")
		return 0
	case "approve":
		var accepted []string
		if tcb != "" {
			for _, s := range strings.Split(tcb, ",") {
				accepted = append(accepted, strings.TrimSpace(s))
			}
		}
		approval, err := pdd.Approve(cwd, plan, reviewer, accepted)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(out, "Recorded approval for", approval.Digest)
		return 0
	case "prove":
		if timeout <= 0 {
			fmt.Fprintln(errOut, "timeout must be positive")
			return 2
		}
		if jar != "" {
			jar, err = filepath.Abs(jar)
			if err != nil {
				return fail(err)
			}
		}
		receipt, dir, err := pdd.Prove(ctx, cwd, plan, pdd.Options{Version: version, Runner: pdd.ToolRunner{Go: goExe, GobraJar: jar, Timeout: timeout}, Tools: pdd.ToolInfo(ctx, goExe, jar), Progress: func(s string) { fmt.Fprintln(errOut, s) }})
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(out, "Receipt:", filepath.Join(dir, "receipt.md"))
		for _, r := range receipt.Requirements {
			fmt.Fprintf(out, "%s: %s (%s)\n", r.ID, r.Status, r.Kind)
		}
		for _, gap := range receipt.Gaps {
			fmt.Fprintln(errOut, "gap:", gap)
		}
		if !receipt.Success {
			return 1
		}
		return 0
	case "status":
		receipt, dir, err := pdd.Status(cwd, plan)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(out, "Current receipt: %s\nSuccess: %t\n", filepath.Join(dir, "receipt.md"), receipt.Success)
		if !receipt.Success {
			return 1
		}
		return 0
	}
	return 2
}
