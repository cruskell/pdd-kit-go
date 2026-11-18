package pdd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	browserArtifactLimit = 8 << 20
	browserArtifactCount = 8
)

var browserProjectRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func isEmpirical(c Check) bool { return c.Kind == "go-test" || c.Kind == "playwright" }

func browserRelativeFile(name string) bool {
	return fs.ValidPath(name) && name != "." && !strings.ContainsAny(name, "\\:\r\n\x00")
}

func validateBrowserCheck(root string, c Check) error {
	if c.Package != "" || len(c.Files) != 0 || !browserRelativeFile(c.Config) || !browserProjectRE.MatchString(c.Project) || len(c.Tests) == 0 {
		return fmt.Errorf("playwright needs a local config file, a single named project and explicit tests")
	}
	config, err := Local(root, c.Config)
	if err != nil {
		return err
	}
	st, err := os.Stat(config)
	if err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("missing Playwright config %s", c.Config)
	}
	seen := map[string]bool{}
	for _, name := range c.Tests {
		file, title, ok := strings.Cut(name, " :: ")
		if !ok || !browserRelativeFile(file) || strings.TrimSpace(title) == "" || strings.ContainsAny(title, "\r\n\x00") || seen[name] {
			return fmt.Errorf("invalid or duplicate Playwright test %q; use file :: title", name)
		}
		seen[name] = true
	}
	return nil
}

// Playwright reports spec.file relative to its config rootDir. Nested describe
// titles form part of a test's identity, as does the single selected project.
type browserSuite struct {
	Title string
	Specs []struct {
		Title, File string
		Tests       []struct {
			ProjectName, ExpectedStatus, Status string
			Results                             []struct {
				Status string
				Retry  int
			}
		}
	}
	Suites []browserSuite
}

func validateBrowserReport(data []byte, c Check, workspace string) error {
	if err := uniqueJSONKeys(data); err != nil {
		return fmt.Errorf("invalid Playwright JSON report: %w", err)
	}
	var report struct {
		Config struct{ Version, RootDir string }
		Suites []browserSuite
		Errors []json.RawMessage
		Stats  struct{ Expected, Skipped, Unexpected, Flaky int }
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("invalid Playwright JSON report: %w", err)
	}
	if report.Config.Version == "" || len(report.Errors) > 0 || report.Stats.Skipped != 0 || report.Stats.Unexpected != 0 || report.Stats.Flaky != 0 {
		return fmt.Errorf("Playwright report contains errors, skipped, unexpected or flaky tests, or lacks runner identity")
	}
	reportRoot := ""
	if workspace != "" {
		rel, err := filepath.Rel(workspace, report.Config.RootDir)
		if err != nil || !filepath.IsAbs(report.Config.RootDir) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("Playwright report root is outside the checked snapshot")
		}
		reportRoot = rel
	}
	passed := map[string]bool{}
	var visit func(browserSuite, string) error
	visit = func(suite browserSuite, prefix string) error {
		for _, spec := range suite.Specs {
			file := strings.ReplaceAll(spec.File, "\\", "/")
			name := file + " :: " + prefix + spec.Title
			if !browserRelativeFile(file) || strings.TrimSpace(spec.Title) == "" || len(spec.Tests) != 1 || passed[name] {
				return fmt.Errorf("invalid or duplicate Playwright spec %q", name)
			}
			if workspace != "" {
				rel := filepath.ToSlash(filepath.Join(reportRoot, filepath.FromSlash(file)))
				item, err := Local(workspace, rel)
				if err != nil {
					return err
				}
				info, err := os.Stat(item)
				if err != nil || !info.Mode().IsRegular() {
					return fmt.Errorf("Playwright test file %q is not in the checked snapshot", rel)
				}
			}
			test := spec.Tests[0]
			if test.ProjectName != c.Project || test.ExpectedStatus != "passed" || test.Status != "expected" || len(test.Results) != 1 || test.Results[0].Status != "passed" || test.Results[0].Retry != 0 {
				return fmt.Errorf("Playwright test %q did not pass on its first execution in project %q", name, c.Project)
			}
			passed[name] = true
		}
		for _, child := range suite.Suites {
			if err := visit(child, prefix+child.Title+" › "); err != nil {
				return err
			}
		}
		return nil
	}
	for _, suite := range report.Suites {
		if err := visit(suite, ""); err != nil {
			return err
		}
	}
	if len(passed) == 0 || len(passed) != report.Stats.Expected {
		return fmt.Errorf("Playwright report lacks a complete passing test count")
	}
	for _, name := range c.Tests {
		if !passed[name] {
			return fmt.Errorf("required Playwright test %q did not execute and pass", name)
		}
	}
	return nil
}

func browserReportFile(c Check) string { return c.ID + ".playwright.json" }

func validBrowserArtifactName(c Check, name string) bool {
	if c.Kind != "playwright" {
		return false
	}
	if name == browserReportFile(c) {
		return true
	}
	for n := 1; n <= browserArtifactCount; n++ {
		prefix := fmt.Sprintf("%s.browser-%02d", c.ID, n)
		if name == prefix+".png" || name == prefix+".zip" {
			return true
		}
	}
	return false
}

func (r ToolRunner) runBrowser(ctx context.Context, root string, c Check) Execution {
	cli := os.Getenv("PDD_PLAYWRIGHT_CLI")
	if !filepath.IsAbs(cli) || filepath.Base(cli) != "cli.js" {
		return Execution{Status: "unavailable", ExitCode: -1, Detail: "set PDD_PLAYWRIGHT_CLI to the absolute path of a provisioned @playwright/test/cli.js"}
	}
	packageDir := filepath.Dir(cli)
	modulesDir := filepath.Dir(filepath.Dir(packageDir))
	if filepath.Base(packageDir) != "test" || filepath.Base(filepath.Dir(packageDir)) != "@playwright" || filepath.Base(modulesDir) != "node_modules" {
		return Execution{Status: "unavailable", ExitCode: -1, Detail: "PDD_PLAYWRIGHT_CLI must point to node_modules/@playwright/test/cli.js"}
	}
	info, err := os.Stat(cli)
	if err != nil || !info.Mode().IsRegular() {
		return Execution{Status: "unavailable", ExitCode: -1, Detail: "provisioned Playwright CLI file is unavailable"}
	}
	output, err := createScratch("pdd-browser-")
	if err != nil {
		return Execution{Status: "failed", ExitCode: -1, Detail: err.Error()}
	}
	defer removeScratch(output)
	reportPath := filepath.Join(output, "report.json")
	artifactsPath := filepath.Join(output, "test-results")
	command := []string{"node", cli, "test", "--config=" + c.Config, "--project=" + c.Project, "--reporter=json", "--output=" + artifactsPath, "--retries=0", "--repeat-each=1", "--forbid-only", "--trace=retain-on-failure", "--no-deps"}
	// Leave the separately provisioned toolchain outside the hashed source tree.
	e := executeWithInput(ctx, root, command, r.Timeout, nil, "NODE_PATH="+modulesDir, "PLAYWRIGHT_JSON_OUTPUT_FILE="+reportPath, "PLAYWRIGHT_FORCE_TTY=0", "CI=1")
	e.artifacts = map[string][]byte{}
	data, err := readArtifact(reportPath, browserArtifactLimit)
	if err == nil {
		e.artifacts[browserReportFile(c)] = data
		if e.Status == "passed" {
			err = validateBrowserReport(data, c, root)
		}
	}
	if e.Status == "passed" && err != nil {
		e.Status = "failed"
		e.Detail = "Playwright evidence: " + err.Error()
	}
	if e.Status != "passed" {
		saveBrowserDiagnostics(artifactsPath, c.ID, e.artifacts)
	}
	return e
}

// Keep only bounded browser files the user can open as screenshots or with the
// Playwright trace viewer. Fresh output lives outside the checkout snapshot.
func saveBrowserDiagnostics(root, id string, artifacts map[string][]byte) {
	remaining := int64(24 << 20)
	visited := 0
	_ = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > 1000 || len(artifacts) >= browserArtifactCount || remaining <= 0 {
			return fs.SkipAll
		}
		if entry.IsDir() {
			return nil
		}
		ext := path.Ext(filepath.ToSlash(file))
		if ext != ".png" && filepath.Base(file) != "trace.zip" {
			return nil
		}
		limit := int64(browserArtifactLimit)
		if remaining < limit {
			limit = remaining
		}
		data, err := readArtifact(file, limit)
		if err == nil {
			artifacts[fmt.Sprintf("%s.browser-%02d%s", id, len(artifacts)+1, ext)] = data
			remaining -= int64(len(data))
		}
		return nil
	})
}
