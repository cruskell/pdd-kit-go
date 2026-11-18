package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestUsageAndDraftWorkflow(t *testing.T) {
	root := t.TempDir()
	var out, errOut bytes.Buffer
	run := func(args ...string) int {
		t.Helper()
		out.Reset()
		errOut.Reset()
		return Run(append([]string{"--cwd", root}, args...), &out, &errOut, "test")
	}
	if run("init") != 0 {
		t.Fatal(errOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".pdd/plan.json")); err != nil {
		t.Fatal(err)
	}
	if run("init") != 1 {
		t.Fatal("init overwrote existing plan")
	}
	if run("lint") != 1 {
		t.Fatal("unfinished scaffold passed lint")
	}
	if run("approve", "--reviewer", "someone") != 1 {
		t.Fatal("unfinished scaffold approved")
	}
	if run("prove") != 1 {
		t.Fatal("unfinished scaffold executed")
	}
	if run("unknown") != 2 {
		t.Fatal("unknown command exit code")
	}
	if run("lint", "--unknown") != 2 {
		t.Fatal("unknown option exit code")
	}
	if run("--help") != 0 || out.Len() == 0 {
		t.Fatal("missing help")
	}
	if run("--version") != 0 || out.String() != "test\n" {
		t.Fatal("version")
	}
}

func decodeResponse(t *testing.T, out *bytes.Buffer, code int) Response {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(out.Bytes()))
	decoder.DisallowUnknownFields()
	var response Response
	if err := decoder.Decode(&response); err != nil {
		t.Fatalf("invalid JSON response: %v\n%s", err, out.String())
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("stdout contains extra output: %s", out.String())
	}
	if response.Schema != 1 || response.ExitCode != code || response.OK != (code == 0) {
		t.Fatalf("response disagrees with process outcome: %+v code=%d", response, code)
	}
	return response
}
func TestJSONSuccessFailureAndNamedScaffolds(t *testing.T) {
	root := t.TempDir()
	var out, diagnostics bytes.Buffer
	run := func(args ...string) (int, Response) {
		t.Helper()
		out.Reset()
		diagnostics.Reset()
		code := Run(append([]string{"--cwd", root, "--json"}, args...), &out, &diagnostics, "test")
		return code, decodeResponse(t, &out, code)
	}
	code, response := run("--plan", "billing", "init")
	if code != 0 || response.Plan != "billing" {
		t.Fatal(response)
	}
	if _, err := os.Stat(filepath.Join(root, ".pdd/plans/billing.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".pdd/plan.json")); !os.IsNotExist(err) {
		t.Fatal("named init touched the default plan")
	}
	for _, args := range [][]string{{"--plan", "billing", "init"}, {"--plan", "billing", "lint"}, {"--plan", "billing", "approve", "--reviewer", "test"}, {"--plan", "billing", "prove"}, {"--plan", "missing", "status"}} {
		code, response = run(args...)
		if code != 1 || response.Error == nil {
			t.Fatalf("expected structured failure: %+v", response)
		}
	}
	for _, args := range [][]string{{"unknown"}, {"lint", "--unknown"}, {"--plan", "../escape", "init"}, {"prove", "--timeout", "invalid"}} {
		code, response = run(args...)
		if code != 2 || response.Error == nil || response.Error.Kind != "usage" {
			t.Fatalf("expected usage failure: %+v", response)
		}
	}
	code, response = run("plans")
	if code != 0 {
		t.Fatal(response)
	}
	data := response.Data.([]interface{})
	if len(data) != 1 || data[0].(map[string]interface{})["valid"] != false {
		t.Fatal("invalid draft was hidden from plan inventory")
	}
	code, response = run("--version")
	if code != 0 || response.Command != "version" {
		t.Fatal(response)
	}
	code, response = run("--help")
	if code != 0 || response.Command != "help" {
		t.Fatal(response)
	}
}
func TestGlobalsDoNotConsumeCommandOptionValues(t *testing.T) {
	globals, args, err := parseGlobals([]string{"approve", "--reviewer", "--json", "--plan", "auth"}, "/work")
	if err != nil || globals.json || globals.plan != "auth" || len(args) != 3 || args[2] != "--json" {
		t.Fatalf("command value reinterpreted as a flag: %+v %v %v", globals, args, err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestBrokenOutputCannotReportSuccess(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--json", "--version"}} {
		if code := Run(args, failingWriter{}, io.Discard, "test"); code != 1 {
			t.Fatal("output write failure returned success")
		}
	}
}

func TestMachineReadableEmpiricalWorkflowEndToEnd(t *testing.T) {
	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.20\n")
	write("core.go", "package fixture\nfunc Allowed(value bool) bool { return value }\n")
	write("core_test.go", "package fixture\nimport \"testing\"\nfunc TestEvidence(t *testing.T) { if !Allowed(true)||Allowed(false){t.Fatal(\"incorrect decision\")} }\n")
	write("template.json", `{"schema":1,"name":"CLI smoke","intent":"The decision preserves its boolean input.","non_goals":["Formal verification"],"requirements":[{"id":"decision","text":"The production function preserves its input on the checked cases.","kind":"empirical","check":"tests"}],"checks":[{"id":"tests","kind":"go-test","package":".","tests":["TestEvidence"]}],"trusted_boundary":[],"mutations":[]}`)
	var out, diagnostics bytes.Buffer
	run := func(args ...string) (int, Response) {
		t.Helper()
		out.Reset()
		diagnostics.Reset()
		code := Run(append([]string{"--cwd", root, "--plan", "smoke", "--json"}, args...), &out, &diagnostics, "test")
		return code, decodeResponse(t, &out, code)
	}
	code, response := run("init", "--from", "template.json")
	if code != 0 {
		t.Fatal(response)
	}
	code, response = run("plan")
	if code != 0 {
		t.Fatal(response)
	}
	digest := response.Data.(map[string]interface{})["digest"].(string)
	code, response = run("approve", "--reviewer", "cli-smoke", "--expect-digest", digest)
	if code != 0 {
		t.Fatal(response)
	}
	code, response = run("export-approval", "--to", ".pdd/reviews/smoke.json")
	if code != 0 {
		t.Fatal(response)
	}
	originalApproval := response.Data.(map[string]interface{})
	if err := os.Remove(filepath.Join(root, ".pdd/state/smoke/approval.json")); err != nil {
		t.Fatal(err)
	}
	code, response = run("import-approval", "--from", ".pdd/reviews/smoke.json")
	if code != 0 {
		t.Fatal(response)
	}
	restoredApproval := response.Data.(map[string]interface{})
	if restoredApproval["reviewer"] != originalApproval["reviewer"] || restoredApproval["approved_at"] != originalApproval["approved_at"] {
		t.Fatal("import minted a new assertion")
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	code, response = run("prove", "--go", goBinary)
	if code != 0 {
		t.Fatalf("prove: %+v\n%s", response, diagnostics.String())
	}
	data := response.Data.(map[string]interface{})
	receipt := data["receipt"].(map[string]interface{})
	if receipt["plan_id"] != "smoke" || receipt["success"] != true {
		t.Fatal("receipt lost selected plan identity or success")
	}
	code, response = run("status")
	if code != 0 {
		t.Fatal(response)
	}
	write("core.go", "package fixture\nfunc Allowed(value bool) bool { return false }\n")
	code, response = run("status")
	if code != 1 || response.Error.Kind != "status" || response.Data.(map[string]interface{})["current"] != false {
		t.Fatalf("stale receipt reported as current: %+v", response)
	}
	code, response = run("prove", "--go", goBinary)
	if code != 1 || response.Error.Kind != "verification" {
		t.Fatalf("broken production behavior did not fail verification: %+v", response)
	}
	code, response = run("status")
	if code != 1 || response.Error.Kind != "verification" || response.Data.(map[string]interface{})["current"] != true {
		t.Fatalf("current negative evidence was not inspectable: %+v", response)
	}
}

func TestSignedPolicyCLIEndToEnd(t *testing.T) {
	root := t.TempDir()
	keys := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\ngo 1.20\n")
	write("core.go", "package fixture\nfunc Allowed() bool {return true}\n")
	write("core_test.go", "package fixture\nimport \"testing\"\nfunc TestEvidence(t *testing.T){if !Allowed(){t.Fatal(\"denied\")}}\n")
	write("template.json", `{"schema":1,"name":"Signed CI","intent":"Allow the checked request.","non_goals":["Formal verification"],"requirements":[{"id":"decision","text":"Allow the checked request.","kind":"empirical","check":"tests"}],"checks":[{"id":"tests","kind":"go-test","package":".","tests":["TestEvidence"]}],"trusted_boundary":[],"mutations":[]}`)
	var out, diagnostics bytes.Buffer
	run := func(args ...string) (int, Response) {
		t.Helper()
		out.Reset()
		diagnostics.Reset()
		code := Run(append([]string{"--cwd", root, "--json"}, args...), &out, &diagnostics, "test")
		return code, decodeResponse(t, &out, code)
	}
	if code, response := run("init", "--from", "template.json"); code != 0 {
		t.Fatal(response)
	}
	secret := filepath.Join(keys, "private.pem")
	public := filepath.Join(keys, "public.pem")
	code, response := run("keygen", "--private-key", secret, "--public-key", public)
	if code != 0 {
		t.Fatal(response)
	}
	identity := response.Data.(map[string]interface{})
	policy := map[string]interface{}{"schema": 1, "max_age_hours": 24, "reviewers": []interface{}{map[string]interface{}{"name": "maintainer", "public_key": identity["public_key"], "plans": []string{"default"}}}}
	encoded, _ := json.Marshal(policy)
	path := filepath.Join(keys, "policy.json")
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	code, response = run("plan")
	if code != 0 {
		t.Fatal(response)
	}
	digest := response.Data.(map[string]interface{})["digest"].(string)
	code, response = run("approve", "--reviewer", "maintainer", "--expect-digest", digest, "--signing-key", secret, "--policy", path)
	if code != 0 {
		t.Fatal(response)
	}
	code, response = run("prove")
	if code != 1 {
		t.Fatal("signature downgraded without policy", response)
	}
	t.Setenv("PDD_REVIEW_POLICY", path)
	code, response = run("prove")
	if code != 0 {
		t.Fatalf("strict proof: %+v\n%s", response, diagnostics.String())
	}
	if response.Data.(map[string]interface{})["receipt"].(map[string]interface{})["approval_policy"] == nil {
		t.Fatal("missing policy evidence")
	}
	code, response = run("status")
	if code != 0 {
		t.Fatal(response)
	}
	if err := os.WriteFile(path, []byte(`{"schema":1,"max_age_hours":24,"reviewers":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	code, response = run("status")
	if code != 1 || response.Data.(map[string]interface{})["current"] != false {
		t.Fatal("revocation did not fail status", response)
	}
}

func TestExplicitProjectDirectoryAliasIsCanonicalized(t *testing.T) {
	root := t.TempDir()
	alias := filepath.Join(t.TempDir(), "project-alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip("symlink privilege unavailable:", err)
	}
	var out, diagnostics bytes.Buffer
	if code := Run([]string{"--cwd", alias, "--json", "init"}, &out, &diagnostics, "test"); code != 0 {
		t.Fatalf("explicit project alias rejected: %s %s", out.String(), diagnostics.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".pdd/plan.json")); err != nil {
		t.Fatal(err)
	}
}

func TestPlanningCommandsHandleInvalidDraftAndJSON(t *testing.T) {
	root := t.TempDir()
	var out, diagnostics bytes.Buffer
	run := func(args ...string) (int, Response) {
		out.Reset()
		diagnostics.Reset()
		code := Run(append([]string{"--cwd", root, "--json"}, args...), &out, &diagnostics, "test")
		return code, decodeResponse(t, &out, code)
	}
	if code, _ := run("init"); code != 0 {
		t.Fatal(diagnostics.String())
	}
	if code, response := run("decisions"); code != 0 || response.Command != "decisions" {
		t.Fatal("cannot inspect an unfinished draft")
	}
	path := filepath.Join(t.TempDir(), "review.html")
	if code, _ := run("report", "--out", path); code != 0 {
		t.Fatal(diagnostics.String())
	}
	if b, err := os.ReadFile(path); err != nil || !bytes.Contains(b, []byte("Before approval")) {
		t.Fatal("missing draft dashboard")
	}
	if code, _ := run("decide", "--id", "x", "--option", "y", "--reason", "--json"); code == 0 {
		t.Fatal("unreviewed decision accepted")
	}
	if code, _ := run("draft"); code == 0 {
		t.Fatal("missing request accepted")
	}
	if code, _ := run("build"); code == 0 {
		t.Fatal("unapproved scaffold built")
	}
	if code, _ := run("pr", "--create"); code == 0 {
		t.Fatal("unverified scaffold published")
	}
	if code, _ := run("review", "--help"); code != 0 {
		t.Fatal("help failed")
	}
}
