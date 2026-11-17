package pdd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const coreSource = `package fixture
// @ ensures result == (present && matches && !locked)
func Decide(present, matches, locked bool) (result bool) {
 return present && matches && !locked
}
`

func fixture(t *testing.T) (string, Plan) {
	t.Helper()
	root := t.TempDir()
	put := func(p, s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, p), []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	put("go.mod", "module fixture\n\ngo 1.20\n")
	put("core.go", coreSource)
	put("core_test.go", `package fixture
import "testing"
func TestBoundary(t *testing.T) { if !Decide(true,true,false) || Decide(true,false,false) { t.Fatal("bad boundary") } }
func TestSkipped(t *testing.T) { t.Skip("not evidence") }
`)
	p := Plan{Schema: 1, Name: "Login", Intent: "Accept iff valid and unlocked", NonGoals: []string{"External identity checks"},
		Requirements: []Requirement{{ID: "decision", Text: "Both directions", Kind: "formal", Check: "proof", File: "core.go", Function: "Decide", Ensures: "result == (present && matches && !locked)"}, {ID: "boundary", Text: "Boundary wiring", Kind: "empirical", Check: "tests"}},
		Checks:       []Check{{ID: "proof", Kind: "gobra", Files: []string{"core.go"}}, {ID: "tests", Kind: "go-test", Package: ".", Tests: []string{"TestBoundary"}}},
		Trust:        []Trust{{ID: "identity", Assumption: "Inputs are truthful", Check: "tests"}},
		Mutations:    []Mutation{{ID: "deny-all", Requirement: "decision", Check: "proof", File: "core.go", Find: "return present && matches && !locked", Replace: "return false"}},
	}
	if err := WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(root); err != nil {
		t.Fatal(err)
	}
	return root, p
}
func approveFixture(t *testing.T, root string, p Plan) {
	t.Helper()
	if _, err := Approve(root, p, "test-reviewer", []string{"identity"}); err != nil {
		t.Fatal(err)
	}
}
func TestApprovalBindsIntentAndContracts(t *testing.T) {
	root, p := fixture(t)
	if _, err := Approve(root, p, "reviewer", nil); err == nil {
		t.Fatal("TCB must be explicitly accepted")
	}
	approveFixture(t, root, p)
	if err := CheckApproval(root, p); err != nil {
		t.Fatal(err)
	}
	changed := p
	changed.Intent = "Different promise"
	if CheckApproval(root, changed) == nil {
		t.Fatal("changed intent retained approval")
	}
	// Implementation changes alone do not weaken or change approved contracts.
	body := strings.Replace(coreSource, "return present && matches && !locked", "return false", 1)
	if err := os.WriteFile(filepath.Join(root, "core.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if err := CheckApproval(root, p); err != nil {
		t.Fatalf("body-only change invalidated contract approval: %v", err)
	}
	body = strings.Replace(body, "// @ ensures result == (present && matches && !locked)", "// @ ensures true", 1)
	if err := os.WriteFile(filepath.Join(root, "core.go"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if CheckApproval(root, p) == nil {
		t.Fatal("weakened contract retained approval")
	}
}
func TestApprovalBindsDeclarationsAndRejectsBypasses(t *testing.T) {
	for _, change := range []struct{ name, source string }{
		{"signature", strings.Replace(coreSource, "present, matches, locked bool", "present, matches, locked, extra bool", 1)},
		{"constant", coreSource + "\nconst policy = false\n"},
	} {
		t.Run(change.name, func(t *testing.T) {
			root, p := fixture(t)
			approveFixture(t, root, p)
			if err := os.WriteFile(filepath.Join(root, "core.go"), []byte(change.source), 0644); err != nil {
				t.Fatal(err)
			}
			if CheckApproval(root, p) == nil {
				t.Fatal("declaration change retained approval")
			}
		})
	}
	for _, source := range []string{
		strings.Replace(coreSource, "// @ ensures", "// @ trusted\n// @ ensures", 1),
		strings.Replace(coreSource, "return present", "// @ assume false\n return present", 1),
		"package fixture\nimport \"os\"\n" + strings.TrimPrefix(coreSource, "package fixture\n"),
	} {
		root, p := fixture(t)
		if err := os.WriteFile(filepath.Join(root, "core.go"), []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
		if p.Validate(root) == nil {
			t.Fatal("unsupported trusted boundary accepted")
		}
	}
}
func TestPlanRejectsHoles(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Plan)
	}{
		{"no mutation", func(p *Plan) { p.Mutations = nil }},
		{"missing test name", func(p *Plan) { p.Checks[1].Tests = nil }},
		{"arbitrary command", func(p *Plan) { p.Checks[1].Kind = "shell" }},
		{"unmapped clause", func(p *Plan) { p.Requirements[0].Ensures = "true" }},
		{"outside package", func(p *Plan) { p.Checks[1].Package = "./../escape" }},
		{"mutate contract", func(p *Plan) {
			p.Mutations[0].Find = "ensures result == (present && matches && !locked)"
			p.Mutations[0].Replace = "ensures true"
		}},
		{"broken mutant", func(p *Plan) { p.Mutations[0].Replace = "return (" }},
		{"no-op mutant", func(p *Plan) { p.Mutations[0].Replace = p.Mutations[0].Find }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, p := fixture(t)
			tt.change(&p)
			if p.Validate(root) == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
}
func TestNamedTestsMustExecute(t *testing.T) {
	root, _ := fixture(t)
	runner := ToolRunner{Timeout: 20 * time.Second}
	for _, test := range []struct{ name, status string }{{"TestBoundary", "passed"}, {"TestMissing", "failed"}, {"TestSkipped", "failed"}} {
		got := runner.Run(context.Background(), root, Check{ID: "tests", Kind: "go-test", Package: ".", Tests: []string{test.name}})
		if got.Status != test.status {
			t.Fatalf("%s: %s %s\n%s", test.name, got.Status, got.Detail, got.Output)
		}
	}
}
func TestToolFailuresAreNotProof(t *testing.T) {
	root, _ := fixture(t)
	got := ToolRunner{Go: "/pdd/no-such-go", Timeout: time.Second}.Run(context.Background(), root, Check{Kind: "go-test", Package: ".", Tests: []string{"TestBoundary"}})
	if got.Status != "unavailable" {
		t.Fatalf("missing tool: %+v", got)
	}
	for _, e := range []Execution{{Status: "timeout", ExitCode: -1}, {Status: "failed", ExitCode: 1, Output: "syntax error"}, {Status: "failed", ExitCode: 1, Output: "Postcondition might not hold. Assertion unrelated might not hold."}} {
		if RejectedMutation(e, "result == true") {
			t.Fatal("tool/parse/unrelated failure counted as killed mutant")
		}
	}
}

type fakeRunner struct {
	survive bool
	touch   bool
	root    string
	calls   int
}

func (f *fakeRunner) Run(_ context.Context, root string, c Check) Execution {
	f.calls++
	e := Execution{Status: "passed", ExitCode: 0, Command: []string{"test-double"}, Output: "baseline evidence"}
	if f.touch && f.calls == 1 {
		_ = os.WriteFile(filepath.Join(f.root, "new.go"), []byte("package fixture\n"), 0644)
	}
	b, _ := os.ReadFile(filepath.Join(root, "core.go"))
	if strings.Contains(string(b), "return false") && !f.survive {
		e.Status = "failed"
		e.ExitCode = 1
		e.Output = "Postcondition might not hold. Assertion result == (present && matches && !locked) might not hold."
	}
	return e
}
func TestReceiptsAndStaleness(t *testing.T) {
	root, p := fixture(t)
	approveFixture(t, root, p)
	r, dir, err := Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}, Version: "test"})
	if err != nil || !r.Success {
		t.Fatalf("prove: %v %+v", err, r.Gaps)
	}
	if r.Requirements[0].Status != "proved" || r.Requirements[1].Status != "evidenced" {
		t.Fatal("verification kinds were conflated")
	}
	if _, _, err := Status(root, p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, r.Evidence[0].Log), []byte("altered"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Status(root, p); err == nil {
		t.Fatal("altered evidence accepted")
	}
	_, _, err = Prove(context.Background(), root, p, Options{Runner: &fakeRunner{}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "extra.txt"), []byte("source changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Status(root, p); err == nil {
		t.Fatal("stale receipt accepted")
	}
}
func TestSurvivingMutantAndConcurrentEditsFail(t *testing.T) {
	for _, mode := range []string{"survive", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			root, p := fixture(t)
			approveFixture(t, root, p)
			runner := &fakeRunner{survive: mode == "survive", touch: mode == "concurrent", root: root}
			r, _, err := Prove(context.Background(), root, p, Options{Runner: runner})
			if err != nil {
				t.Fatal(err)
			}
			if r.Success || len(r.Gaps) == 0 {
				t.Fatal("gap reported success")
			}
		})
	}
}
func TestSymlinksAndUnknownFieldsRejected(t *testing.T) {
	root, p := fixture(t)
	if err := os.Symlink(filepath.Join(root, "core.go"), filepath.Join(root, "linked.go")); err != nil {
		t.Skip(err)
	}
	p.Checks[0].Files = []string{"linked.go"}
	if p.Validate(root) == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := Capture(root); err == nil {
		t.Fatal("snapshot followed symlink")
	}
	if err := os.WriteFile(filepath.Join(root, PlanPath), []byte(`{"schema":1,"unexpected":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("unknown plan field silently ignored")
	}
}
func TestGobraIntegration(t *testing.T) {
	if os.Getenv("PDD_INTEGRATION") != "1" {
		t.Skip("set PDD_INTEGRATION=1 to run real pinned Gobra Docker image")
	}
	root, p := fixture(t)
	approveFixture(t, root, p)
	r, _, err := Prove(context.Background(), root, p, Options{Runner: ToolRunner{Timeout: 90 * time.Second}, Version: "integration"})
	if err != nil || !r.Success {
		for _, evidence := range r.Evidence {
			t.Logf("%s: %s", evidence.Check, evidence.Execution.Output)
		}
		t.Fatalf("real Gobra + Go checks: %v %v", err, r.Gaps)
	}
	if r.Mutations[0].Status != "killed" {
		t.Fatal("real mutant was not rejected")
	}
	fmt.Println("Gobra verified production Go and rejected deny-everyone mutant")
}

func TestGoDiagnosticsDoNotCorruptJSONEvidence(t *testing.T) {
	if os.PathSeparator != '/' {
		t.Skip("Unix executable wrapper fixture")
	}
	root, _ := fixture(t)
	wrapper := filepath.Join(t.TempDir(), "go-wrapper")
	// A real Go invocation can write dependency-download notices to stderr.
	// This fixture reproduces that separate diagnostic channel without networking.
	source := "#!/bin/sh\nprintf 'go: downloading example.org/dependency v1.0.0\\n' >&2\nprintf '%s\\n' '{\"Action\":\"pass\",\"Test\":\"TestBoundary\"}'\n"
	if err := os.WriteFile(wrapper, []byte(source), 0755); err != nil {
		t.Fatal(err)
	}
	result := ToolRunner{Go: wrapper, Timeout: time.Second}.Run(context.Background(), root, Check{Kind: "go-test", Package: ".", Tests: []string{"TestBoundary"}})
	if result.Status != "passed" || !strings.Contains(result.Output, "downloading") {
		t.Fatalf("stdout/stderr separation lost valid evidence or diagnostics: %+v", result)
	}
}
