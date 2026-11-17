package pdd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Execution struct {
	Status     string   `json:"status"` // passed, failed, unavailable, timeout
	Command    []string `json:"command"`
	ExitCode   int      `json:"exit_code"`
	Output     string   `json:"-"`
	stdout     string
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}
type Runner interface {
	Run(context.Context, string, Check) Execution
}
type ToolRunner struct {
	Go       string
	GobraJar string
	Timeout  time.Duration
}

var formalPassRE = regexp.MustCompile(`(?i)Gobra found 0 errors\.`)

func (r ToolRunner) Run(ctx context.Context, root string, c Check) Execution {
	goExe := r.Go
	if goExe == "" {
		goExe = "go"
	}
	var command []string
	if c.Kind == "gobra" && strconv.IntSize != 64 {
		return Execution{Status: "unavailable", ExitCode: -1, Detail: "the pinned Gobra adapter currently requires a 64-bit host"}
	}
	if c.Kind == "go-test" {
		var patterns []string
		for _, t := range c.Tests {
			patterns = append(patterns, regexp.QuoteMeta(t))
		}
		command = []string{goExe, "test", "-json", "-count=1", "-run", "^(" + strings.Join(patterns, "|") + ")$", c.Package}
	} else if r.GobraJar != "" {
		args, err := gobraArguments(root, c, root)
		if err != nil {
			return Execution{Status: "failed", ExitCode: -1, Detail: err.Error()}
		}
		command = append([]string{"java", "-Xss128m", "-jar", r.GobraJar}, args...)
	} else {
		// The image's entrypoint runs /gobra/gobra.jar. The checkout is read-only.
		containerName := fmt.Sprintf("pdd-%d-%d", os.Getpid(), time.Now().UnixNano())
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = exec.CommandContext(cleanup, "docker", "rm", "-f", containerName).Run()
		}()
		args, err := gobraArguments(root, c, "/workspace")
		if err != nil {
			return Execution{Status: "failed", ExitCode: -1, Detail: err.Error()}
		}
		command = append([]string{"docker", "run", "--rm", "--name", containerName, "--network=none", "--mount", "type=bind,src=" + root + ",dst=/workspace,readonly", GobraImage}, args...)
	}
	e := execute(ctx, root, command, r.Timeout)
	if e.Status != "passed" {
		return e
	}
	if c.Kind == "go-test" {
		if err := validateTests(e.stdout, c.Tests); err != nil {
			e.Status = "failed"
			e.Detail = err.Error()
		}
	} else if !formalPassRE.MatchString(e.Output) || !strings.Contains(e.Output, "Verifying package") || strings.Contains(strings.ToLower(e.Output), "verification failed") {
		e.Status = "failed"
		e.Detail = "Gobra exited zero without a verification run and success summary"
	}
	return e
}

// boundedOutput continues draining stdout/stderr after its memory cap.
// Concurrent writes come from os/exec's output copier goroutines.
type boundedOutput struct {
	mu        sync.Mutex
	b         bytes.Buffer
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remain := (4 << 20) - b.b.Len()
	if len(p) > remain {
		p = p[:remain]
		b.truncated = true
	}
	b.b.Write(p)
	return n, nil
}
func execute(ctx context.Context, root string, command []string, timeout time.Duration) Execution {
	e := Execution{Status: "failed", Command: command, ExitCode: -1}
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = root
	configureProcess(cmd)
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
	var output, diagnostics boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &diagnostics
	start := time.Now()
	err := cmd.Run()
	cleanupProcess(cmd, err)
	e.DurationMS = time.Since(start).Milliseconds()
	e.stdout = output.b.String()
	e.Output = e.stdout
	if diagnostics.b.Len() > 0 {
		e.Output += "\n[stderr]\n" + diagnostics.b.String()
	}
	truncated := output.truncated || diagnostics.truncated || len(e.Output) > 4<<20
	if truncated {
		e.Output += "\n[pdd: output truncated]\n"
	}
	if ctx.Err() != nil {
		e.Status = "timeout"
		if errors.Is(ctx.Err(), context.Canceled) {
			e.Status = "canceled"
		}
		e.Detail = ctx.Err().Error()
		return e
	}
	if err != nil {
		if ex, ok := err.(*exec.ExitError); ok {
			e.ExitCode = ex.ExitCode()
			if e.ExitCode == 125 || e.ExitCode == 126 || e.ExitCode == 127 {
				e.Status = "unavailable"
			}
		} else if errors.Is(err, exec.ErrWaitDelay) {
			e.Status = "failed"
		} else {
			e.Status = "unavailable"
		}
		e.Detail = err.Error()
		return e
	}
	e.ExitCode = 0
	if truncated {
		e.Detail = "output exceeded 4 MiB; cannot safely assess result"
		return e
	}
	e.Status = "passed"
	return e
}
func validateTests(output string, names []string) error {
	passed := map[string]bool{}
	decoder := json.NewDecoder(strings.NewReader(output))
	for {
		var event struct{ Action, Test, Output string }
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid go test JSON: %w", err)
		}
		if event.Action == "pass" && event.Test != "" {
			passed[event.Test] = true
		}
	}
	for _, name := range names {
		if !passed[name] {
			return fmt.Errorf("required test %s did not execute and pass (missing or skipped)", name)
		}
	}
	return nil
}

// RejectedMutation requires a postcondition diagnostic for the target file and
// the exact authored clause line. Matching expression text elsewhere is not a
// rejection of this requirement's mutant.
func RejectedMutation(e Execution, requirement Requirement, source []byte) bool {
	if e.Status != "failed" || e.ExitCode <= 0 {
		return false
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, requirement.File, source, parser.ParseComments)
	if err != nil {
		return false
	}
	line := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Doc == nil {
			continue
		}
		name, err := symbolName(fn)
		if err != nil || name != requirement.Function {
			continue
		}
		for _, comment := range fn.Doc.List {
			if annotation(comment.Text) == "ensures "+requirement.Ensures {
				line = fset.Position(comment.Pos()).Line
			}
		}
	}
	if line == 0 {
		return false
	}
	for _, match := range mutationDiagnosticRE.FindAllStringSubmatch(stripANSI(e.Output), -1) {
		path := strings.ReplaceAll(match[1], "\\", "/")
		diagnosticLine, _ := strconv.Atoi(match[2])
		if (path == requirement.File || strings.HasSuffix(path, "/"+requirement.File)) && diagnosticLine == line && normalizedAssertion(match[3]) == normalizedAssertion(requirement.Ensures) {
			return true
		}
	}
	return false
}

var mutationDiagnosticRE = regexp.MustCompile(`(?s)Error at:\s*<([^>\n]+):([0-9]+):[0-9]+>\s*Postcondition might not hold\.\s*Assertion (.*?) might not hold\.`)

func normalizedAssertion(value string) string { return strings.Join(strings.Fields(value), " ") }
func stripANSI(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`).ReplaceAllString(s, "")
}

// ToolInfo records versions outside the proof result; failures are diagnostic.
func ToolInfo(ctx context.Context, goExe, jar string) map[string]string {
	if goExe == "" {
		goExe = "go"
	}
	result := map[string]string{"gobra_image": GobraImage, "integer_overflow_checks": "enabled", "injectivity_checks": "enabled"}
	for name, cmd := range map[string][]string{"go": {goExe, "version"}, "java": {"java", "-version"}, "docker": {"docker", "--version"}} {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		b, err := exec.CommandContext(c, cmd[0], cmd[1:]...).CombinedOutput()
		cancel()
		if err == nil {
			scanner := bufio.NewScanner(bytes.NewReader(b))
			if scanner.Scan() {
				result[name] = scanner.Text()
			}
		} else {
			result[name] = "unavailable"
		}
	}
	if jar != "" {
		if b, err := os.ReadFile(jar); err == nil {
			result["gobra_jar_sha256"] = hashBytes(b)
		}
		result["gobra_image"] = "local jar"
	}
	result["word_bits"] = strconv.Itoa(strconv.IntSize)
	result["go_target"] = runtime.GOOS + "/" + runtime.GOARCH
	return result
}

// sourceRoot is the host checkout for Java and its read-only mount for Docker.
func gobraArguments(root string, c Check, sourceRoot string) ([]string, error) {
	args := []string{"--overflow", "--noassumeInjectivityOnInhale"}
	imports := false
	for _, file := range c.Files {
		b, err := readSource(root, file)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, b, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		imports = imports || len(f.Imports) > 0
	}
	if imports {
		module, err := mainModule(root)
		if err != nil {
			return nil, err
		}
		args = append(args, "--module", module, "-I", sourceRoot)
	}
	args = append(args, "-i")
	for _, file := range c.Files {
		args = append(args, filepath.ToSlash(filepath.Join(sourceRoot, filepath.FromSlash(file))))
	}
	return args, nil
}
