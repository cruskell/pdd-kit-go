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
	"syscall"
	"time"

	"github.com/cruskell/pdd-kit-go/internal/pdd"
)

const help = `pdd — Proof-Driven Development for Go

Usage: pdd [--cwd DIR] [--plan NAME] [--json] <command> [options]

  init       Create an unapproved plan scaffold without overwriting a plan
  plans      List valid and invalid plans in this project
  plan       Review intent, contracts, checks and trust; --diff shows changes
  lint       Validate the selected plan and production Go contract mappings
  keygen     Generate an Ed25519 keypair (keep the private key outside the project)
  approve    Record review: --reviewer NAME [--accept-tcb id1,id2]
  export-approval  Save the current reviewed assertion: --to PATH
  import-approval  Restore a matching reviewed assertion: --from PATH
  prove      Run approved Gobra proofs, named Go tests and negative mutations
  status     Check whether this plan's latest receipt is current and successful
  doctor     Check Go and Docker/Java prerequisites
  draft      Generate a plan from --request-file; --apply installs an unapproved draft
  decisions  Inspect decision cards, including unresolved drafts
  decide     Answer a card: --id ID --option ID --reason TEXT --expect-draft HASH
  review     Independent advisory review: --role both|intent|guarantees|gap
  tasks      Show check dependencies, evidence and remaining workflow gaps
  build      Repair/verify/review loop; --apply installs a verified candidate
  report     Write an offline HTML review dashboard: --out PATH
  pr         Preview an evidence-backed PR; --create explicitly creates a draft PR

Agent options: --driver codex or --worker EXECUTABLE (JSON stdin/stdout)
              --files a.go,b.go --agent-timeout 5m
Build options: --max-iterations 3 --go PATH --gobra-jar PATH --timeout 2m
Review agents are advisory. Human approval and machine proof remain mandatory.

Global: --policy PATH (or PDD_REVIEW_POLICY) enforces authorized, unexpired signatures
Global: --cwd DIR, --plan NAME (default: default), --json, --version, --help
Named plans: .pdd/plans/NAME.json; state: .pdd/state/NAME/
The default plan keeps .pdd/plan.json and .pdd/approval.json.
init:       --from PATH (optional; imports a plan without executing it)
plan:       --diff
approve:    --reviewer NAME --accept-tcb ID[,ID...] --expect-digest SHA256
            Optional --signing-key PATH signs that reviewed digest.
            Review plan --json first; --expect-digest guards review-time changes.
prove:      --go PATH --gobra-jar PATH --timeout 2m
            Default backend: pinned Gobra Docker image. Missing tools fail closed.
            Project tests run in scratch copies with host user permissions.
--json:     One schema-1 response on stdout, progress on stderr; check ok/exit_code.
Without --policy approval is a local assertion. With --policy, signing keys must be authorized.
`

func Run(args []string, out, errOut io.Writer, version string) (exitCode int) {
	writer := &checkedWriter{writer: out}
	out = writer
	defer func() {
		if writer.err != nil {
			fmt.Fprintln(errOut, "error writing command output:", writer.err)
			exitCode = 1
		}
	}()
	cwd, workingDirectoryErr := os.Getwd()
	global, rest, parseErr := parseGlobals(args, cwd)
	command := "help"
	if len(rest) > 0 {
		command = rest[0]
	}
	printer := output{json: global.json, out: out, diagnostics: errOut, command: command, plan: global.plan}
	if parseErr != nil {
		return printer.finish(2, nil, "usage", parseErr, "")
	}
	paths, err := pdd.PathsFor(global.plan)
	if err != nil {
		return printer.finish(2, nil, "usage", err, "")
	}
	printer.plan = paths.Name
	if len(rest) == 0 || command == "--help" || command == "-h" || command == "help" {
		printer.command = "help"
		if !global.json {
			fmt.Fprint(out, help)
		}
		return printer.finish(0, map[string]string{"help": help}, "", nil, "")
	}
	if command == "--version" {
		printer.command = "version"
		if len(rest) > 1 {
			return printer.finish(2, nil, "usage", fmt.Errorf("unexpected arguments after --version"), "")
		}
		if !global.json {
			fmt.Fprintln(out, version)
		}
		return printer.finish(0, map[string]string{"version": version}, "", nil, "")
	}
	if workingDirectoryErr != nil && global.cwd == "" {
		return printer.finish(1, nil, "io", workingDirectoryErr, "Pass an absolute --cwd path to an existing project.")
	}
	cwd, err = filepath.Abs(global.cwd)
	if err != nil {
		return printer.finish(2, nil, "usage", err, "")
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return printer.finish(2, nil, "usage", fmt.Errorf("cannot resolve project directory: %w", err), "")
	}
	stat, err := os.Stat(cwd)
	if err != nil || !stat.IsDir() {
		return printer.finish(2, nil, "usage", fmt.Errorf("--cwd must name an existing directory"), "")
	}
	if workflowCommand(command) {
		return runWorkflow(cwd, global, rest, printer, out, errOut, version)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		if !global.json {
			fmt.Fprint(out, help)
		}
	}
	var reviewer, accepted, expected, goExe, jar, from, to, signingKey, privateKey, publicKey string
	var timeout time.Duration
	var diff bool
	switch command {
	case "keygen":
		fs.StringVar(&privateKey, "private-key", "", "New private key path (outside project)")
		fs.StringVar(&publicKey, "public-key", "", "New public key path")
	case "approve":
		fs.StringVar(&signingKey, "signing-key", "", "Ed25519 PKCS8 private key PEM")
		fs.StringVar(&reviewer, "reviewer", "", "Reviewer assertion")
		fs.StringVar(&accepted, "accept-tcb", "", "Comma-separated trust IDs")
		fs.StringVar(&expected, "expect-digest", "", "Digest of the intent and contracts reviewed")
	case "plan":
		fs.BoolVar(&diff, "diff", false, "Show changes since recorded review")
	case "prove", "doctor":
		fs.StringVar(&goExe, "go", "go", "Go executable")
		fs.StringVar(&jar, "gobra-jar", "", "Local Gobra jar instead of Docker")
		fs.DurationVar(&timeout, "timeout", 2*time.Minute, "Timeout per check")
	case "export-approval":
		fs.StringVar(&to, "to", "", "Destination for the existing reviewed assertion")
	case "import-approval":
		fs.StringVar(&from, "from", "", "Previously reviewed approval artifact")
	case "init":
		fs.StringVar(&from, "from", "", "Import a schema-1 plan file as an unapproved draft")
	case "plans", "lint", "status":
	default:
		return printer.finish(2, nil, "usage", fmt.Errorf("unknown command: %s", command), "Run pdd --help for commands.")
	}
	if err = fs.Parse(rest[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return printer.finish(0, map[string]string{"help": help}, "", nil, "")
		}
		return printer.finish(2, nil, "usage", err, "")
	}
	if fs.NArg() > 0 {
		return printer.finish(2, nil, "usage", fmt.Errorf("unexpected argument: %s", fs.Arg(0)), "")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "doctor" {
		return doctor(ctx, goExe, jar, printer)
	}
	if command == "plans" {
		plans, err := pdd.ListPlans(cwd)
		if err != nil {
			return printer.finish(1, nil, "io", err, "")
		}
		if !global.json {
			for _, plan := range plans {
				status := "valid"
				if !plan.Valid {
					status = "invalid"
				}
				fmt.Fprintf(out, "%s: %s (%s)\n", plan.ID, status, plan.File)
				if plan.Error != "" {
					fmt.Fprintln(out, "  "+plan.Error)
				}
			}
		}
		return printer.finish(0, plans, "", nil, "")
	}
	if command == "init" {
		result, err := pdd.Initialize(cwd, paths.Name, from)
		if err != nil {
			return printer.finish(1, nil, "initialization", err, "Select another --plan name for an independent plan; init never overwrites an existing plan.")
		}
		if !global.json {
			fmt.Fprintln(out, "Created", result.Path, "(draft; no approval was recorded)")
			if result.ValidationError != "" {
				fmt.Fprintln(out, "Before approval:", result.ValidationError)
			}
		}
		return printer.finish(0, result, "", nil, "")
	}
	resolve := func(path string) string {
		if path != "" && !filepath.IsAbs(path) {
			return filepath.Join(cwd, path)
		}
		return path
	}
	global.policy = resolve(global.policy)
	signingKey = resolve(signingKey)
	if command == "keygen" {
		if privateKey == "" || publicKey == "" {
			return printer.finish(2, nil, "usage", fmt.Errorf("keygen requires --private-key and --public-key"), "")
		}
		privateKey = resolve(privateKey)
		publicKey = resolve(publicKey)
		rel, e := filepath.Rel(cwd, privateKey)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return printer.finish(2, nil, "usage", fmt.Errorf("store private keys outside the project so source snapshots cannot copy them"), "")
		}
		identity, err := pdd.GenerateSigningKey(privateKey, publicKey)
		if err != nil {
			return printer.finish(1, nil, "signing", err, "")
		}
		if !global.json {
			fmt.Fprintln(out, "Created Ed25519 signing identity", identity.KeyID)
		}
		return printer.finish(0, identity, "", nil, "")
	}
	plan, err := pdd.LoadNamed(cwd, paths.Name)
	if err != nil {
		return printer.finish(1, nil, "validation", err, "Check the selected plan file and its contract mappings; use init for a new plan.")
	}
	switch command {
	case "plan":
		report, err := pdd.ReportReview(cwd, plan)
		if err != nil {
			return printer.finish(1, nil, "review", err, "")
		}
		if !global.json {
			renderReview(out, report, diff)
		}
		return printer.finish(0, report, "", nil, "")
	case "lint":
		digest, err := pdd.Digest(cwd, plan)
		if err != nil {
			return printer.finish(1, nil, "validation", err, "")
		}
		if !global.json {
			fmt.Fprintln(out, "Plan, production contracts, trust mappings and mutations are structurally valid.")
		}
		return printer.finish(0, map[string]interface{}{"valid": true, "digest": digest, "plan_path": paths.File, "requirements": len(plan.Requirements), "checks": len(plan.Checks)}, "", nil, "")
	case "approve":
		var trust []string
		if accepted != "" {
			for _, id := range strings.Split(accepted, ",") {
				trust = append(trust, strings.TrimSpace(id))
			}
		}
		approval, err := pdd.ApproveSignedWithPolicy(cwd, plan, reviewer, trust, expected, signingKey, global.policy)
		if err != nil {
			return printer.finish(1, nil, "approval", err, "Review plan --diff and plan --json before recording approval.")
		}
		if !global.json {
			fmt.Fprintln(out, "Recorded approval for", approval.Digest)
		}
		return printer.finish(0, approval, "", nil, "")
	case "export-approval", "import-approval":
		if (command == "export-approval" && to == "") || (command == "import-approval" && from == "") {
			return printer.finish(2, nil, "usage", fmt.Errorf("%s requires --to or --from respectively", command), "")
		}
		var approval pdd.Approval
		if command == "export-approval" {
			approval, err = pdd.ExportApprovalWithPolicy(cwd, plan, to, global.policy)
		} else {
			approval, err = pdd.ImportApprovalWithPolicy(cwd, plan, from, global.policy)
		}
		if err != nil {
			return printer.finish(1, nil, "approval", err, "Import only a complete saved review matching this plan; changed intent requires review and approval.")
		}
		if !global.json {
			fmt.Fprintf(out, "%s: preserved approval %s by %s\n", command, approval.Digest, approval.Reviewer)
		}
		return printer.finish(0, approval, "", nil, "")
	case "prove":
		if timeout <= 0 {
			return printer.finish(2, nil, "usage", fmt.Errorf("timeout must be positive"), "")
		}
		if err = pdd.CheckApproval(cwd, plan); err != nil {
			return printer.finish(1, nil, "approval", err, "Review the selected plan and explicitly accept its trusted boundary before proving.")
		}
		if jar != "" {
			jar, err = filepath.Abs(jar)
			if err != nil {
				return printer.finish(2, nil, "usage", err, "")
			}
		}
		receipt, dir, err := pdd.Prove(ctx, cwd, plan, pdd.Options{PolicyPath: global.policy, Version: version, Runner: pdd.ToolRunner{Go: goExe, GobraJar: jar, Timeout: timeout}, Tools: pdd.ToolInfo(ctx, goExe, jar), Progress: func(message string) { fmt.Fprintln(errOut, message) }})
		if err != nil {
			return printer.finish(1, nil, "execution", err, "")
		}
		_, latest, currentErr := pdd.StatusContext(ctx, cwd, plan, global.policy)
		current := currentErr == nil && latest == dir
		data := receiptResult(receipt, dir, current)
		if !global.json {
			fmt.Fprintln(out, "Receipt:", filepath.Join(dir, "receipt.md"))
			for _, verdict := range receipt.Requirements {
				fmt.Fprintf(out, "%s: %s (%s)\n", verdict.ID, verdict.Status, verdict.Kind)
			}
			for _, gap := range receipt.Gaps {
				fmt.Fprintln(errOut, "gap:", gap)
			}
		}
		if !receipt.Success {
			return printer.finish(1, data, "verification", fmt.Errorf("verification failed with %d gap(s)", len(receipt.Gaps)), "Inspect receipt gaps and per-check logs; failed proofs are never replaced by passing tests.")
		}
		if !current {
			if currentErr == nil {
				currentErr = fmt.Errorf("a newer verification replaced this result")
			}
			return printer.finish(1, data, "status", currentErr, "Inspect status or rerun prove for current evidence.")
		}
		return printer.finish(0, data, "", nil, "")
	case "status":
		receipt, dir, err := pdd.StatusContext(ctx, cwd, plan, global.policy)
		data := receiptResult(receipt, dir, err == nil)
		if err != nil {
			return printer.finish(1, data, "status", err, "Run prove against the current approved plan to obtain current evidence.")
		}
		if !global.json {
			fmt.Fprintf(out, "Current receipt: %s\nSuccess: %t\n", filepath.Join(dir, "receipt.md"), receipt.Success)
		}
		if !receipt.Success {
			return printer.finish(1, data, "verification", fmt.Errorf("latest verification did not succeed"), "Inspect the latest receipt's gaps and logs.")
		}
		return printer.finish(0, data, "", nil, "")
	}
	return printer.finish(2, nil, "usage", fmt.Errorf("unknown command"), "")
}

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

func doctor(ctx context.Context, goExe, jar string, printer output) int {
	checks := []doctorCheck{}
	commands := []struct {
		name string
		args []string
	}{{"go", []string{goExe, "version"}}, {"gobra-container", []string{"docker", "image", "inspect", pdd.GobraImage, "--format", "{{.Id}}"}}}
	if jar != "" {
		commands[1] = struct {
			name string
			args []string
		}{"java", []string{"java", "-version"}}
		stat, err := os.Stat(jar)
		ok := err == nil && !stat.IsDir()
		detail := jar
		if err != nil {
			detail = err.Error()
		}
		checks = append(checks, doctorCheck{"gobra-jar", ok, detail})
	}
	for _, command := range commands {
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		bytes, err := exec.CommandContext(probe, command.args[0], command.args[1:]...).CombinedOutput()
		cancel()
		detail := strings.TrimSpace(string(bytes))
		if err != nil {
			detail = err.Error() + ": " + detail
		}
		checks = append(checks, doctorCheck{command.name, err == nil, detail})
	}
	good := true
	for _, check := range checks {
		good = good && check.OK
		if !printer.json {
			fmt.Fprintf(printer.out, "%s: %s\n", check.Name, check.Detail)
		}
	}
	data := map[string]interface{}{"checks": checks, "docker_pull": "docker pull " + pdd.GobraImage}
	if !good {
		return printer.finish(1, data, "prerequisite", fmt.Errorf("one or more verification prerequisites are unavailable"), "For Docker backend: docker pull "+pdd.GobraImage)
	}
	return printer.finish(0, data, "", nil, "")
}

type receiptOutput struct {
	Receipt *pdd.Receipt `json:"receipt,omitempty"`
	Path    string       `json:"path,omitempty"`
	Current bool         `json:"current"`
}

func receiptResult(receipt pdd.Receipt, dir string, current bool) receiptOutput {
	result := receiptOutput{Current: current}
	if receipt.Schema > 0 {
		result.Receipt = &receipt
	}
	if dir != "" {
		result.Path = filepath.Join(dir, "receipt.json")
	}
	return result
}
