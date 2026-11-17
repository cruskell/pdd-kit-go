// Package pdd coordinates approved Go contracts, Gobra proofs and empirical checks.
package pdd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const PlanPath = ".pdd/plan.json"
const ApprovalPath = ".pdd/approval.json"

// GobraImage is pinned so proof receipts identify a reproducible verifier.
const GobraImage = "ghcr.io/viperproject/gobra@sha256:5e96431b06c2e6209aa767df9153bb4cf8b2620598598f1d7d3f3a74c09c14f9"

type Plan struct {
	Inputs       *InputPolicy `json:"inputs,omitempty"`
	selectedName string
	Schema       int           `json:"schema"`
	Name         string        `json:"name"`
	Intent       string        `json:"intent"`
	NonGoals     []string      `json:"non_goals"`
	Requirements []Requirement `json:"requirements"`
	Checks       []Check       `json:"checks"`
	Trust        []Trust       `json:"trusted_boundary"`
	Mutations    []Mutation    `json:"mutations"`
}
type Requirement struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Kind     string `json:"kind"` // formal or empirical
	Check    string `json:"check"`
	File     string `json:"file,omitempty"`
	Function string `json:"function,omitempty"`
	Ensures  string `json:"ensures,omitempty"`
}
type Check struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // gobra or go-test; arbitrary commands are not accepted
	Files   []string `json:"files,omitempty"`
	Package string   `json:"package,omitempty"`
	Tests   []string `json:"tests,omitempty"`
}
type Trust struct {
	ID         string `json:"id"`
	Assumption string `json:"assumption"`
	Check      string `json:"check"`
}
type Mutation struct {
	ID          string `json:"id"`
	Requirement string `json:"requirement"`
	Check       string `json:"check"`
	File        string `json:"file"`
	Find        string `json:"find"`
	Replace     string `json:"replace"`
}
type Approval struct {
	Signature     *ApprovalSignature `json:"signature,omitempty"`
	Schema        int                `json:"schema"`
	Digest        string             `json:"digest"`
	Reviewer      string             `json:"reviewer"`
	AcceptedTrust []string           `json:"accepted_trust"`
	ApprovedAt    time.Time          `json:"approved_at"`
	PlanID        string             `json:"plan_id,omitempty"`
	Review        *ReviewBundle      `json:"review,omitempty"`
}

var idRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var testRE = regexp.MustCompile(`^Test[A-Z0-9_][a-zA-Z0-9_]*$`)

func ReadJSON(path string, v interface{}) error {
	data, err := readArtifact(path, 8<<20)
	if err != nil {
		return err
	}
	if err = uniqueJSONKeys(data); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var extra interface{}
	if err = d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%s: trailing JSON data", path)
	}
	return nil
}
func Load(root string) (Plan, error) { return LoadNamed(root, "default") }

func Local(root, path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return "", fmt.Errorf("path must be repository-relative: %q", path)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == "." {
		return "", fmt.Errorf("path escapes repository or names its root: %q", path)
	}
	first := strings.Split(filepath.ToSlash(clean), "/")[0]
	if first == ".git" || first == ".pdd" || first == "bin" || first == "dist" {
		return "", fmt.Errorf("reserved path: %q", path)
	}
	abs := filepath.Join(root, clean)
	if err := NoSymlinks(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// NoSymlinks prevents reads and writes through symlinked artifact paths.
func NoSymlinks(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported: %s", p)
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}
func readSource(root, path string) ([]byte, error) {
	abs, err := Local(root, path)
	if err != nil {
		return nil, err
	}
	return readArtifact(abs, 256<<20)
}
func (p Plan) Validate(root string) error {
	if _, err := effectivePolicy(p.Inputs); err != nil {
		return err
	}
	if p.Schema != 1 {
		return fmt.Errorf("only plan schema 1 is supported")
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Intent) == "" {
		return fmt.Errorf("name and intent are required")
	}
	if len(p.Requirements) == 0 || len(p.Checks) == 0 {
		return fmt.Errorf("at least one requirement and check are required")
	}
	ids := map[string]bool{}
	add := func(id string) error {
		if !idRE.MatchString(id) || ids[id] {
			return fmt.Errorf("invalid or duplicate id %q", id)
		}
		ids[id] = true
		return nil
	}
	checks := map[string]Check{}
	formalFiles := map[string]bool{}
	for _, c := range p.Checks {
		if err := add(c.ID); err != nil {
			return err
		}
		checks[c.ID] = c
		switch c.Kind {
		case "gobra":
			if len(c.Files) == 0 || c.Package != "" || len(c.Tests) > 0 {
				return fmt.Errorf("%s: gobra requires files only", c.ID)
			}
			for _, f := range c.Files {
				if f != filepath.ToSlash(filepath.Clean(f)) {
					return fmt.Errorf("%s: use canonical repository-relative file paths without ./ or ..", c.ID)
				}
				if filepath.Ext(f) != ".go" || strings.HasSuffix(f, "_test.go") {
					return fmt.Errorf("%s: prove production .go files directly", c.ID)
				}
				policy, _ := effectivePolicy(p.Inputs)
				absolute, err := Local(root, f)
				if err != nil {
					return err
				}
				info, err := os.Stat(absolute)
				if err != nil {
					return err
				}
				if info.Size() > policy.MaxFileBytes {
					return fmt.Errorf("%s exceeds the reviewed per-file input limit", f)
				}
				b, err := readSource(root, f)
				if err != nil {
					return err
				}
				if _, err = contracts(f, b); err != nil {
					return err
				}
				formalFiles[f] = true
			}
		case "go-test":
			if len(c.Files) > 0 || len(c.Tests) == 0 {
				return fmt.Errorf("%s: go-test requires a package and explicit test names", c.ID)
			}
			if c.Package != "." && !strings.HasPrefix(c.Package, "./") {
				return fmt.Errorf("%s: test package must be local", c.ID)
			}
			if strings.ContainsAny(c.Package, "\\\x00\r\n") || strings.Contains(c.Package, "...") {
				return fmt.Errorf("%s: test exactly one local package", c.ID)
			}
			if c.Package != "." {
				dir, err := Local(root, c.Package)
				if err != nil {
					return err
				}
				st, err := os.Stat(dir)
				if err != nil || !st.IsDir() {
					return fmt.Errorf("%s: missing test package", c.ID)
				}
			}
			seen := map[string]bool{}
			for _, t := range c.Tests {
				if !testRE.MatchString(t) || seen[t] {
					return fmt.Errorf("%s: invalid or duplicate test name %q", c.ID, t)
				}
				seen[t] = true
			}
		default:
			return fmt.Errorf("%s: unsupported check kind %q", c.ID, c.Kind)
		}
	}
	graph, err := resolvePackages(root, p)
	if err != nil {
		return err
	}
	reqs := map[string]Requirement{}
	used := map[string]bool{}
	clauses := map[string]bool{}
	for _, r := range p.Requirements {
		if err := add(r.ID); err != nil {
			return err
		}
		reqs[r.ID] = r
		if strings.TrimSpace(r.Text) == "" {
			return fmt.Errorf("%s: requirement text is empty", r.ID)
		}
		c, ok := checks[r.Check]
		if !ok {
			return fmt.Errorf("%s: unknown check %s", r.ID, r.Check)
		}
		used[c.ID] = true
		if r.Kind == "formal" {
			if c.Kind != "gobra" || !contains(c.Files, r.File) || r.Function == "" || strings.TrimSpace(r.Ensures) == "" {
				return fmt.Errorf("%s: formal requirement needs a Gobra file, function and ensures clause", r.ID)
			}
			b, err := readSource(root, r.File)
			if err != nil {
				return err
			}
			fn, err := function(r.File, b, r.Function)
			if err != nil {
				return err
			}
			found := false
			if fn.Doc != nil {
				for _, comment := range fn.Doc.List {
					if annotation(comment.Text) == "ensures "+r.Ensures {
						found = true
					}
				}
			}
			if !found {
				return fmt.Errorf("%s: ensures clause is not attached to %s in %s", r.ID, r.Function, r.File)
			}
			key := r.File + "\x00" + r.Function + "\x00" + r.Ensures
			if clauses[key] {
				return fmt.Errorf("%s: ensures clause is mapped twice", r.ID)
			}
			clauses[key] = true
		} else if r.Kind == "empirical" {
			if c.Kind != "go-test" || r.File != "" || r.Function != "" || r.Ensures != "" {
				return fmt.Errorf("%s: empirical requirement must map to go-test", r.ID)
			}
		} else {
			return fmt.Errorf("%s: kind must be formal or empirical", r.ID)
		}
	}
	// Every postcondition in the proof files must be visible in the intent map.
	for file := range formalFiles {
		b, err := readSource(root, file)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, b, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Doc != nil {
				for _, comment := range fn.Doc.List {
					a := annotation(comment.Text)
					if strings.HasPrefix(a, "ensures ") {
						name, err := symbolName(fn)
						if err != nil {
							return err
						}
						key := file + "\x00" + name + "\x00" + strings.TrimPrefix(a, "ensures ")
						if !clauses[key] {
							return fmt.Errorf("%s.%s: unmapped ensures clause", file, name)
						}
					}
				}
			}
		}
	}
	for _, t := range p.Trust {
		if err := add(t.ID); err != nil {
			return err
		}
		if strings.TrimSpace(t.Assumption) == "" || checks[t.Check].Kind != "go-test" {
			return fmt.Errorf("%s: trust assumption requires empirical check", t.ID)
		}
		used[t.Check] = true
	}
	for _, deps := range graph.Dependencies {
		for _, dep := range deps {
			used[dep] = true
		}
	}
	for id := range checks {
		if !used[id] {
			return fmt.Errorf("%s: check has no requirement or trusted boundary", id)
		}
	}
	mutants := map[string]int{}
	for _, m := range p.Mutations {
		if err := add(m.ID); err != nil {
			return err
		}
		r, ok := reqs[m.Requirement]
		if !ok || r.Kind != "formal" || m.Check != r.Check || m.File != r.File {
			return fmt.Errorf("%s: mutation must target its formal requirement's check and production file", m.ID)
		}
		b, err := readSource(root, m.File)
		if err != nil {
			return err
		}
		if _, err = Mutate(m, r, b); err != nil {
			return err
		}
		mutants[r.ID]++
	}
	for _, r := range p.Requirements {
		if r.Kind == "formal" && mutants[r.ID] == 0 {
			return fmt.Errorf("%s: formal clause requires at least one mutation", r.ID)
		}
	}
	if err := validateInputs(root, p); err != nil {
		return err
	}
	return nil
}
func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
func annotation(s string) string {
	s = strings.TrimSpace(strings.TrimPrefix(s, "//"))
	if strings.HasPrefix(s, "@") {
		return strings.TrimSpace(strings.TrimPrefix(s, "@"))
	}
	return ""
}
func function(file string, b []byte, name string) (*ast.FuncDecl, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, b, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			symbol, err := symbolName(fn)
			if err != nil {
				return nil, err
			}
			if symbol == name {
				return fn, nil
			}
		}
	}
	return nil, fmt.Errorf("%s: function %s not found", file, name)
}

// contracts freezes all proof annotations while allowing implementation changes.
// Escape hatches are intentionally rejected in this first backend adapter.
func contracts(file string, b []byte) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, b, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var result []string

	skeleton := append([]byte{}, b...)
	// Freeze signatures, types and constants as well as annotation order. Bodies
	// remain editable; all proof comments, including body invariants, are frozen.
	var bodies []*ast.BlockStmt
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
			bodies = append(bodies, fn.Body)
		}
	}
	for i := len(bodies) - 1; i >= 0; i-- {
		body := bodies[i]
		start, end := int(body.Pos())-1, int(body.End())-1
		skeleton = append(append(append([]byte{}, skeleton[:start]...), []byte("{}")...), skeleton[end:]...)
	}
	// Remove ordinary comments by parsing and printing the bodyless AST.
	sf, parseErr := parser.ParseFile(token.NewFileSet(), file, skeleton, 0)
	if parseErr != nil {
		return nil, parseErr
	}
	var declarations bytes.Buffer
	if err := format.Node(&declarations, token.NewFileSet(), sf); err != nil {
		return nil, err
	}
	banned := regexp.MustCompile(`\b(trusted|assume|inhale|axiom)\b`)
	for _, group := range f.Comments {
		for _, c := range group.List {
			if strings.HasPrefix(c.Text, "/*") && strings.Contains(c.Text, "@") {
				return nil, fmt.Errorf("%s: use single-line // @ annotations", file)
			}
			a := annotation(c.Text)
			if a != "" {
				if banned.MatchString(a) {
					return nil, fmt.Errorf("%s: unverified escape hatch is not supported: %s", file, a)
				}
				result = append(result, a)
			}
		}
	}
	attachedPure := map[token.Pos]bool{}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok {
			if fn.Body == nil {
				return nil, fmt.Errorf("%s: bodyless functions are not supported", file)
			}
			if fn.Doc != nil {
				for _, comment := range fn.Doc.List {
					attachedPure[comment.Pos()] = true
				}
			}
		}
	}
	for _, group := range f.Comments {
		for _, comment := range group.List {
			a := annotation(comment.Text)
			if (a == "pure" || strings.HasPrefix(a, "pure ")) && !attachedPure[comment.Pos()] {
				return nil, fmt.Errorf("%s: pure annotations must attach to a function's doc comment", file)
			}
		}
	}
	result = append(result, "declarations:"+declarations.String())
	return result, nil
}
func Mutate(m Mutation, r Requirement, b []byte) ([]byte, error) {
	if m.Find == "" || m.Find == m.Replace || bytes.Count(b, []byte(m.Find)) != 1 {
		return nil, fmt.Errorf("%s: find must match exactly once and replacement must differ", m.ID)
	}
	fn, err := function(m.File, b, r.Function)
	if err != nil {
		return nil, err
	}
	if fn.Body == nil {
		return nil, fmt.Errorf("missing function body")
	}
	start := bytes.Index(b, []byte(m.Find))
	bodyStart := int(fn.Body.Pos()) - 1
	bodyEnd := int(fn.Body.End()) - 1
	if start <= bodyStart || start+len(m.Find) > bodyEnd {
		return nil, fmt.Errorf("%s: mutation must be inside the target function body", m.ID)
	}
	mutated := bytes.Replace(b, []byte(m.Find), []byte(m.Replace), 1)
	before, err := contracts(m.File, b)
	if err != nil {
		return nil, err
	}
	after, err := contracts(m.File, mutated)
	if err != nil {
		return nil, fmt.Errorf("%s: mutant must remain valid Go: %w", m.ID, err)
	}
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		return nil, fmt.Errorf("%s: mutation cannot alter proof annotations", m.ID)
	}
	return mutated, nil
}
func Approve(root string, p Plan, reviewer string, accepted []string) (Approval, error) {
	return ApproveExpected(root, p, reviewer, accepted, "")
}
func ApproveExpected(root string, p Plan, reviewer string, accepted []string, expected string) (Approval, error) {
	return ApproveSigned(root, p, reviewer, accepted, expected, "")
}
func ApproveSigned(root string, p Plan, reviewer string, accepted []string, expected, signingKey string) (Approval, error) {
	return ApproveSignedWithPolicy(root, p, reviewer, accepted, expected, signingKey, "")
}
func ApproveSignedWithPolicy(root string, p Plan, reviewer string, accepted []string, expected, signingKey, policyPath string) (Approval, error) {
	unlock, err := lockProject(root)
	if err != nil {
		return Approval{}, err
	}
	defer unlock()
	if err = p.Validate(root); err != nil {
		return Approval{}, err
	}
	if err = authoredPlanMatches(root, p); err != nil {
		return Approval{}, err
	}
	if strings.TrimSpace(reviewer) == "" {
		return Approval{}, fmt.Errorf("--reviewer is required; approval records an assertion, not an authenticated identity")
	}
	var want []string
	for _, t := range p.Trust {
		want = append(want, t.ID)
	}
	sort.Strings(want)
	accepted = append([]string{}, accepted...)
	sort.Strings(accepted)
	if strings.Join(want, ",") != strings.Join(accepted, ",") {
		return Approval{}, fmt.Errorf("explicitly accept each trusted boundary with --accept-tcb %s", strings.Join(want, ","))
	}
	review, err := BuildReview(root, p)
	if err != nil {
		return Approval{}, err
	}
	digest, err := review.digest()
	if err != nil {
		return Approval{}, err
	}
	if expected != "" && expected != digest {
		return Approval{}, fmt.Errorf("review digest changed; expected %s, current %s; review again before approving", expected, digest)
	}
	a := Approval{Schema: 1, Digest: digest, Reviewer: reviewer, AcceptedTrust: accepted, ApprovedAt: time.Now().UTC(), PlanID: p.ID(), Review: &review}
	if signingKey != "" {
		if expected == "" {
			return Approval{}, fmt.Errorf("signed approval requires --expect-digest from the review inspected")
		}
		rel, e := filepath.Rel(root, signingKey)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return Approval{}, fmt.Errorf("private signing key must be outside the project")
		}
		if err = signApproval(&a, signingKey); err != nil {
			return Approval{}, err
		}
	}
	if policyPath != "" {
		if _, err = verifyPolicy(policyPath, p, a, time.Now().UTC()); err != nil {
			return Approval{}, err
		}
	}
	return a, WriteJSON(filepath.Join(root, p.stateFile("approval.json")), a)
}
func CheckApproval(root string, p Plan) error {
	if err := authoredPlanMatches(root, p); err != nil {
		return err
	}
	var a Approval
	if err := ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &a); err != nil {
		return fmt.Errorf("plan is not approved: %w", err)
	}
	return validateApproval(root, p, a)
}

func validateApproval(root string, p Plan, a Approval) error {
	if a.Signature != nil {
		if _, err := verifySignature(a); err != nil {
			return err
		}
	}
	digest, err := Digest(root, p)
	if err != nil {
		return err
	}
	if a.Schema != 1 || !matchesPlanID(a.PlanID, p) || a.Digest != digest || strings.TrimSpace(a.Reviewer) == "" || a.ApprovedAt.IsZero() {
		return fmt.Errorf("approval is stale or invalid; review the current plan and contracts")
	}
	if a.Review != nil {
		saved, err := a.Review.digest()
		if err != nil || saved != a.Digest || !matchesPlanID(a.Review.PlanID, p) {
			return fmt.Errorf("saved review does not match approval")
		}
	}
	var want []string
	for _, t := range p.Trust {
		want = append(want, t.ID)
	}
	sort.Strings(want)
	got := append([]string{}, a.AcceptedTrust...)
	sort.Strings(got)
	if strings.Join(want, ",") != strings.Join(got, ",") {
		return fmt.Errorf("trusted boundary approval does not match")
	}
	return nil
}
func WriteJSON(path string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if len(b)+1 > 8<<20 {
		return fmt.Errorf("%s: JSON exceeds 8 MiB; reduce the model or split independent named plans", path)
	}
	return AtomicWrite(path, append(b, '\n'))
}
func AtomicWrite(path string, b []byte) error {
	if err := NoSymlinks(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pdd-write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Pure bodies define terms that can appear in contracts, including contracts in
// importing packages. Changing a definition must require a new review even when
// the text of the calling ensures clause stays identical.
func specificationDefinitions(file string, b []byte) ([]byte, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, b, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	type definition struct {
		Name string
		Body string
	}
	definitions := []definition{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Doc == nil || fn.Body == nil {
			continue
		}
		pure := false
		for _, comment := range fn.Doc.List {
			a := annotation(comment.Text)
			if a == "pure" || strings.HasPrefix(a, "pure ") {
				pure = true
			}
		}
		if pure {
			name, err := symbolName(fn)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition{name, string(b[int(fn.Body.Pos())-1 : int(fn.Body.End())-1])})
		}
	}
	if len(definitions) == 0 {
		return nil, nil
	}
	return json.Marshal(definitions)
}
