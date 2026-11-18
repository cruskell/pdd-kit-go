package pdd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const passingBrowserReport = `{"config":{"version":"1.65.1"},"suites":[{"title":"wallet.spec.cjs","suites":[{"title":"Withdrawal","specs":[{"file":"wallet.spec.cjs","title":"denies overdraft","tests":[{"projectName":"chromium","expectedStatus":"passed","status":"expected","results":[{"status":"passed","retry":0}]}]}]}]}],"errors":[],"stats":{"expected":1,"skipped":0,"unexpected":0,"flaky":0}}`

func TestBrowserReportRequiresNamedFirstPass(t *testing.T) {
	check := Check{Kind: "playwright", Project: "chromium", Tests: []string{"wallet.spec.cjs :: Withdrawal › denies overdraft"}}
	if err := validateBrowserReport([]byte(passingBrowserReport), check, ""); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"empty":            "{}",
		"missing":          strings.Replace(passingBrowserReport, "denies overdraft", "another case", 1),
		"wrong project":    strings.Replace(passingBrowserReport, "chromium", "webkit", 1),
		"expected failure": strings.Replace(passingBrowserReport, `"expectedStatus":"passed"`, `"expectedStatus":"failed"`, 1),
		"retry":            strings.Replace(passingBrowserReport, `"retry":0`, `"retry":1`, 1),
		"skip":             strings.Replace(passingBrowserReport, `"skipped":0`, `"skipped":1`, 1),
		"flaky":            strings.Replace(passingBrowserReport, `"flaky":0`, `"flaky":1`, 1),
		"incomplete":       strings.Replace(passingBrowserReport, `"expected":1`, `"expected":2`, 1),
		"duplicate keys":   strings.Replace(passingBrowserReport, `"expected":1`, `"expected":1,"expected":1`, 1),
	}
	for label, body := range cases {
		t.Run(label, func(t *testing.T) {
			if err := validateBrowserReport([]byte(body), check, ""); err == nil {
				t.Fatal("accepted unsupported Playwright result")
			}
		})
	}
}

type browserFixtureRunner struct {
	fakeRunner
	fail bool
}

func (r *browserFixtureRunner) Run(ctx context.Context, root string, check Check) Execution {
	if check.Kind != "playwright" {
		return r.fakeRunner.Run(ctx, root, check)
	}
	e := Execution{Status: "passed", Output: "Playwright ran", artifacts: map[string][]byte{browserReportFile(check): []byte(passingBrowserReport)}}
	if r.fail {
		e.Status = "failed"
		e.ExitCode = 1
		e.Detail = "browser assertion failed"
		e.artifacts[check.ID+".browser-02.png"] = []byte("test screenshot fixture")
		e.artifacts[check.ID+".browser-03.zip"] = []byte("test trace fixture")
	}
	return e
}

func TestBrowserReceiptAndDashboard(t *testing.T) {
	ctx := context.Background()
	root, plan := fixture(t)
	if err := os.WriteFile(filepath.Join(root, "playwright.config.cjs"), []byte("module.exports = {}"), 0644); err != nil {
		t.Fatal(err)
	}
	check := Check{ID: "browser", Kind: "playwright", Config: "playwright.config.cjs", Project: "chromium", Tests: []string{"wallet.spec.cjs :: Withdrawal › denies overdraft"}}
	plan.Checks = append(plan.Checks, check)
	plan.Requirements = append(plan.Requirements, Requirement{ID: "overdraft", Text: "Denies overdraft", Kind: "empirical", Check: check.ID})
	savePlan(t, root, plan)
	approveFixture(t, root, plan)
	runner := &browserFixtureRunner{}
	receipt, dir, err := Prove(ctx, root, plan, Options{Runner: runner})
	if err != nil || !receipt.Success {
		t.Fatalf("prove: %+v %v", receipt.Gaps, err)
	}
	if _, _, err = StatusContext(ctx, root, plan, ""); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, ".pdd", "results", "report.html")
	report := func() string {
		t.Helper()
		if err := WriteDashboard(ctx, root, plan.ID(), "", output); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	page := report()
	for _, want := range []string{"Checks and logs", "Negative examples", "killed", "evidenced", "playwright · empirical", "../receipts/", "browser.playwright.json", "deny-all.log"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing dashboard evidence %q", want)
		}
	}
	name := browserReportFile(check)
	broken := []byte(strings.Replace(passingBrowserReport, "denies overdraft", "wrong test", 1))
	if err = AtomicWrite(filepath.Join(dir, name), broken); err != nil {
		t.Fatal(err)
	}
	if _, _, err = StatusContext(ctx, root, plan, ""); err == nil {
		t.Fatal("accepted an altered browser report")
	}
	// Even replacing the digest cannot make a report with the wrong test green.
	receipt.Evidence[len(receipt.Evidence)-1].Artifacts[0].SHA256 = hashBytes(broken)
	if err = WriteJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		t.Fatal(err)
	}
	if _, _, err = StatusContext(ctx, root, plan, ""); err == nil {
		t.Fatal("accepted a report missing planned test evidence")
	}
	if page = report(); !strings.Contains(page, "No current evidence") || strings.Contains(page, "browser.playwright.json") {
		t.Fatal("dashboard showed damaged evidence")
	}
	runner.fail = true
	receipt, _, err = Prove(ctx, root, plan, Options{Runner: runner})
	if err != nil || receipt.Success {
		t.Fatalf("failed browser run: success=%t error=%v", receipt.Success, err)
	}
	if _, _, err = StatusContext(ctx, root, plan, ""); err != nil {
		t.Fatal(err)
	}
	page = report()
	for _, want := range []string{"browser assertion failed", "Failure screenshot", "Playwright trace"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing failure evidence %q", want)
		}
	}
}
