package pdd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const policyTypes = `package policy
type Policy struct { Enabled bool; MinimumAge int }
`
const policyMethod = `package policy
// @ ensures allowed == (p.Enabled && age >= p.MinimumAge && !locked)
func (p Policy) Allows(age int, locked bool) (allowed bool) {
 return p.Enabled && age >= p.MinimumAge && !locked
}
`
const policyPointer = `package policy
// @ preserves acc(p)
// @ ensures enabled == p.Enabled
func (p *Policy) IsEnabled() (enabled bool) { return p.Enabled }
`
const handlerSource = `package handler
import "fixture/policy"
// @ ensures result == (enabled && age >= minimum && !locked)
func Authorize(enabled bool, minimum, age int, locked bool) (result bool) {
 p := policy.Policy{Enabled:enabled, MinimumAge:minimum}
 return p.Allows(age,locked)
}
`

func putFile(t *testing.T, root, file, source string) {
	t.Helper()
	path := filepath.Join(root, file)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}
func packagesFixture(t *testing.T) (string, Plan) {
	t.Helper()
	root := t.TempDir()
	putFile(t, root, "go.mod", "module fixture\n\ngo 1.20\n")
	putFile(t, root, "policy/types.go", policyTypes)
	putFile(t, root, "policy/policy.go", policyMethod)
	putFile(t, root, "policy/pointer.go", policyPointer)
	putFile(t, root, "handler/handler.go", handlerSource)
	putFile(t, root, "handler/handler_test.go", `package handler
import ("testing";"fixture/policy")
func TestPolicyPointer(t *testing.T) {for _,enabled:=range []bool{false,true}{p:=policy.Policy{Enabled:enabled};if p.IsEnabled()!=enabled{t.Fatal("pointer read")}}}
func TestAuthorization(t *testing.T) { if !Authorize(true,18,18,false)||Authorize(true,18,17,false)||Authorize(true,18,20,true)||Authorize(false,18,20,false){t.Fatal("authorization boundary failed")} }
`)
	// Author the caller first to verify automatic dependency ordering.
	p := Plan{Schema: 1, Name: "Package authorization", Intent: "Use a verified local policy method to enforce enablement, minimum age and account locks.", NonGoals: []string{"Truth of caller input values"},
		Requirements: []Requirement{
			{ID: "authorize", Text: "The importing production function obeys the policy.", Kind: "formal", Check: "handler-proof", File: "handler/handler.go", Function: "Authorize", Ensures: "result == (enabled && age >= minimum && !locked)"},
			{ID: "policy", Text: "The value-receiver method accepts exactly permitted requests.", Kind: "formal", Check: "policy-proof", File: "policy/policy.go", Function: "Policy.Allows", Ensures: "allowed == (p.Enabled && age >= p.MinimumAge && !locked)"},
			{ID: "pointer", Text: "Accessible policy pointers preserve access and return enablement.", Kind: "formal", Check: "policy-proof", File: "policy/pointer.go", Function: "Policy.IsEnabled", Ensures: "enabled == p.Enabled"},
			{ID: "boundary", Text: "Go production package wiring works on the specified cases.", Kind: "empirical", Check: "boundary-tests"},
		},
		Checks: []Check{{ID: "handler-proof", Kind: "gobra", Files: []string{"handler/handler.go"}}, {ID: "policy-proof", Kind: "gobra", Files: []string{"policy/types.go", "policy/policy.go", "policy/pointer.go"}}, {ID: "boundary-tests", Kind: "go-test", Package: "./handler", Tests: []string{"TestAuthorization", "TestPolicyPointer"}}},
		Mutations: []Mutation{
			{ID: "deny-authorized", Requirement: "authorize", Check: "handler-proof", File: "handler/handler.go", Find: "return p.Allows(age,locked)", Replace: "return false"},
			{ID: "ignore-lock", Requirement: "policy", Check: "policy-proof", File: "policy/policy.go", Find: "return p.Enabled && age >= p.MinimumAge && !locked", Replace: "return p.Enabled && age >= p.MinimumAge"},
			{ID: "invert-pointer", Requirement: "pointer", Check: "policy-proof", File: "policy/pointer.go", Find: "return p.Enabled", Replace: "return !p.Enabled"},
		}, Trust: []Trust{},
	}
	if err := WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(root); err != nil {
		t.Fatal(err)
	}
	return root, p
}
func TestReceiverQualifiedMethodMapping(t *testing.T) {
	source := `package fixture
type A bool
type B bool
// @ ensures result == bool(a)
func (a A) Allows() (result bool) { return bool(a) }
// @ ensures result == bool(*b)
func (b *B) Allows() (result bool) { return bool(*b) }
// @ ensures result
func Allows() (result bool) { return true }
`
	for _, name := range []string{"A.Allows", "B.Allows", "Allows"} {
		fn, err := function("methods.go", []byte(source), name)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := symbolName(fn)
		if got != name {
			t.Fatal(got)
		}
	}
	if _, err := function("methods.go", []byte(source), "C.Allows"); err == nil {
		t.Fatal("method on the wrong receiver was accepted")
	}
	root, p := packagesFixture(t)
	p.Requirements[1].Function = "Allows"
	if p.Validate(root) == nil {
		t.Fatal("method accepted without its receiver identity")
	}
}
func TestCompleteVerifiedPackageGraph(t *testing.T) {
	root, p := packagesFixture(t)
	graph, err := resolvePackages(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Order[0].ID != "policy-proof" || graph.Order[1].ID != "handler-proof" || strings.Join(graph.Dependencies["handler-proof"], ",") != "policy-proof" {
		t.Fatalf("wrong proof order: %+v", graph)
	}
	approve := func() {
		t.Helper()
		if _, err := Approve(root, p, "test-reviewer", nil); err != nil {
			t.Fatal(err)
		}
	}
	approve()
	putFile(t, root, "policy/types.go", strings.Replace(policyTypes, "MinimumAge int", "MinimumAge int; Disabled bool", 1))
	if CheckApproval(root, p) == nil {
		t.Fatal("imported type contract changed without invalidating approval")
	}
}
func TestUnverifiedImportPathsRejected(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, string, *Plan)
	}{
		{"missing dependency check", func(_ *testing.T, _ string, p *Plan) { p.Checks = p.Checks[:1] }},
		{"missing declaration file", func(_ *testing.T, _ string, p *Plan) { p.Checks[1].Files = []string{"policy/policy.go"} }},
		{"missing implementation file", func(_ *testing.T, _ string, p *Plan) { p.Checks[1].Files = []string{"policy/types.go"} }},
		{"duplicate file", func(_ *testing.T, _ string, p *Plan) {
			p.Checks[1].Files = append(p.Checks[1].Files, "policy/types.go")
		}},
		{"two directories", func(_ *testing.T, _ string, p *Plan) {
			p.Checks[1].Files = append(p.Checks[1].Files, "handler/handler.go")
		}},
		{"two checks for one package", func(_ *testing.T, _ string, p *Plan) {
			p.Checks = append(p.Checks, Check{ID: "other-policy", Kind: "gobra", Files: []string{"policy/types.go", "policy/policy.go", "policy/pointer.go"}})
		}},
		{"extra source file", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "policy/unlisted.go", "package policy\nconst extra = true\n")
		}},
		{"shadowing header", func(t *testing.T, root string, _ *Plan) { putFile(t, root, "policy/policy.gobra", "package policy\n") }},
		{"external import", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "handler/handler.go", strings.Replace(handlerSource, "fixture/policy", "external.org/policy", 1))
		}},
		{"dot import", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "handler/handler.go", strings.Replace(handlerSource, "import ", "import . ", 1))
		}},
		{"side effect import", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "handler/handler.go", strings.Replace(handlerSource, "import ", "import _ ", 1))
		}},
		{"module replacement", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "go.mod", "module fixture\nreplace fixture/policy => ../other\n")
		}},
		{"nested module requirement", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "go.mod", "module fixture\nrequire fixture/policy v1.0.0\n")
		}},
		{"nested module", func(t *testing.T, root string, _ *Plan) { putFile(t, root, "policy/go.mod", "module fixture/policy\n") }},
		{"build constrained file", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "policy/policy.go", "//go:build ignore\n\n"+policyMethod)
		}},
		{"different package names", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "policy/types.go", strings.Replace(policyTypes, "package policy", "package other", 1))
		}},
		{"import cycle", func(t *testing.T, root string, _ *Plan) {
			putFile(t, root, "policy/types.go", strings.Replace(policyTypes, "package policy", "package policy\nimport \"fixture/handler\"", 1))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, p := packagesFixture(t)
			tt.change(t, root, &p)
			if p.Validate(root) == nil {
				t.Fatal("incomplete or ambiguous proof graph accepted")
			}
		})
	}
}
func TestPureDefinitionsRequireReview(t *testing.T) {
	root, p := fixture(t)
	source := coreSource + `// @ decreases
// @ pure
func eligible(value bool) (result bool) { return value }
`
	putFile(t, root, "core.go", source)
	if err := p.Validate(root); err != nil {
		t.Fatal(err)
	}
	approveFixture(t, root, p)
	putFile(t, root, "core.go", strings.Replace(source, "return value", "return true", 1))
	if CheckApproval(root, p) == nil {
		t.Fatal("changed pure definition retained contract approval")
	}
}

type dependencyRunner struct {
	mode  string
	calls []string
}

func (r *dependencyRunner) Run(_ context.Context, root string, c Check) Execution {
	r.calls = append(r.calls, c.ID)
	e := Execution{Status: "passed", ExitCode: 0, Output: "baseline", Command: []string{"test-double"}}
	policy, _ := os.ReadFile(filepath.Join(root, "policy/policy.go"))
	handler, _ := os.ReadFile(filepath.Join(root, "handler/handler.go"))
	pointer, _ := os.ReadFile(filepath.Join(root, "policy/pointer.go"))
	mutatedPolicy := !strings.Contains(string(policy), "return p.Enabled && age >= p.MinimumAge && !locked")
	mutatedHandler := strings.Contains(string(handler), "return false")
	if c.ID == "policy-proof" && r.mode == "baseline-failed" {
		e.Status = "failed"
		e.ExitCode = 1
		e.Output = "baseline verification failed"
	}
	if c.ID == "policy-proof" && mutatedPolicy && r.mode != "mutant-survived" {
		e.Status = "failed"
		e.ExitCode = 1
		e.Output = "Postcondition might not hold. Assertion allowed == (p.Enabled && age >= p.MinimumAge && !locked) might not hold."
	}
	if c.ID == "policy-proof" && strings.Contains(string(pointer), "return !p.Enabled") && r.mode != "mutant-survived" {
		e.Status = "failed"
		e.ExitCode = 1
		e.Output = "Postcondition might not hold. Assertion enabled == p.Enabled might not hold."
	}
	if c.ID == "handler-proof" && mutatedHandler {
		e.Status = "failed"
		e.ExitCode = 1
		e.Output = "Postcondition might not hold. Assertion result == (enabled && age >= minimum && !locked) might not hold."
	}
	return e
}
func TestDependencyFailuresPropagate(t *testing.T) {
	for _, mode := range []string{"passed", "baseline-failed", "mutant-survived"} {
		t.Run(mode, func(t *testing.T) {
			root, p := packagesFixture(t)
			if _, err := Approve(root, p, "test-reviewer", nil); err != nil {
				t.Fatal(err)
			}
			runner := &dependencyRunner{mode: mode}
			receipt, _, err := Prove(context.Background(), root, p, Options{Runner: runner})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "passed" {
				if !receipt.Success {
					t.Fatal(receipt.Gaps)
				}
				if _, _, err := Status(root, p); err != nil {
					t.Fatal(err)
				}
				return
			}
			if receipt.Success || receipt.Requirements[0].Status != "unproven" {
				t.Fatalf("caller was proved despite dependency gap: %+v", receipt.Requirements)
			}
			if mode == "baseline-failed" {
				for _, call := range runner.calls {
					if call == "handler-proof" {
						t.Fatal("ran caller proof after dependency baseline failed")
					}
				}
			}
		})
	}
}
func TestGobraPackagesIntegration(t *testing.T) {
	if os.Getenv("PDD_INTEGRATION") != "1" {
		t.Skip("set PDD_INTEGRATION=1 for real imported-package and method proofs")
	}
	// Verify the shipped plan and production sources rather than another copy
	// of the fixture. This catches examples drifting from CLI mappings.
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := Capture(repository)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = snapshot.Materialize(root); err != nil {
		t.Fatal(err)
	}
	var p Plan
	if err = ReadJSON(filepath.Join(root, "examples/authorization/plan.json"), &p); err != nil {
		t.Fatal(err)
	}
	if err = WriteJSON(filepath.Join(root, PlanPath), p); err != nil {
		t.Fatal(err)
	}
	var trust []string
	for _, item := range p.Trust {
		trust = append(trust, item.ID)
	}
	if _, err = Approve(root, p, "integration-demo", trust); err != nil {
		t.Fatal(err)
	}
	receipt, _, err := Prove(context.Background(), root, p, Options{Runner: ToolRunner{Timeout: 90 * time.Second}, Version: "integration"})
	if err != nil || !receipt.Success {
		for _, e := range receipt.Evidence {
			t.Logf("%s: %s", e.Check, e.Execution.Output)
		}
		for _, m := range receipt.Mutations {
			t.Logf("%s: %s", m.ID, m.Execution.Output)
		}
		t.Fatalf("package proof: %v %v", err, receipt.Gaps)
	}
	if _, _, err := Status(root, p); err != nil {
		t.Fatal(err)
	}
}

func TestDeclarationOnlyDependenciesAreCovered(t *testing.T) {
	root, p := packagesFixture(t)
	putFile(t, root, "shared/types.go", "package shared\ntype Switch = bool\n")
	putFile(t, root, "policy/types.go", strings.Replace(strings.Replace(policyTypes, "package policy", "package policy\nimport \"fixture/shared\"", 1), "Enabled bool", "Enabled shared.Switch", 1))
	p.Checks = append(p.Checks, Check{ID: "shared-types", Kind: "gobra", Files: []string{"shared/types.go"}})
	if err := p.Validate(root); err != nil {
		t.Fatal(err)
	}
	graph, err := resolvePackages(root, p)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Order[0].ID != "shared-types" || strings.Join(graph.Dependencies["policy-proof"], ",") != "shared-types" {
		t.Fatal("transitive type dependency was not verified first")
	}
	p.Checks = p.Checks[:len(p.Checks)-1]
	if p.Validate(root) == nil {
		t.Fatal("unverified transitive dependency accepted")
	}
}

func TestDetachedPureAnnotationsRejected(t *testing.T) {
	root, p := fixture(t)
	putFile(t, root, "core.go", coreSource+"\n// @ pure\n\nfunc hiddenMeaning(value bool) bool { return value }\n")
	if p.Validate(root) == nil {
		t.Fatal("pure definition could escape approval binding through detached comments")
	}
}
