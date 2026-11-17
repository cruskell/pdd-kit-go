package pdd

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// proofPackages resolves only packages owned by the main module. Gobra imports
// use contracts modularly, so every local callee package must have its own full
// proof check. No imported header/stub can silently extend the trusted base.
type proofPackages struct {
	Module       string
	Dependencies map[string][]string
	Order        []Check
}

func resolvePackages(root string, p Plan) (proofPackages, error) {
	graph := proofPackages{Dependencies: map[string][]string{}}
	byDir := map[string]Check{}
	packageNames := map[string]string{}
	for _, c := range p.Checks {
		if c.Kind != "gobra" {
			continue
		}
		if len(c.Files) == 0 {
			return graph, fmt.Errorf("%s: missing proof files", c.ID)
		}
		dir := filepath.ToSlash(filepath.Dir(c.Files[0]))
		if other, ok := byDir[dir]; ok {
			return graph, fmt.Errorf("%s and %s: use one complete Gobra check per package", other.ID, c.ID)
		}
		byDir[dir] = c
		listed := map[string]bool{}
		for _, file := range c.Files {
			if filepath.ToSlash(filepath.Dir(file)) != dir {
				return graph, fmt.Errorf("%s: proof files must be in one directory", c.ID)
			}
			match, err := build.Default.MatchFile(filepath.Join(root, filepath.FromSlash(dir)), filepath.Base(file))
			if err != nil || !match {
				return graph, fmt.Errorf("%s: proof file is excluded by Go's host build rules", file)
			}
			if listed[file] {
				return graph, fmt.Errorf("%s: duplicate proof file %s", c.ID, file)
			}
			listed[file] = true
			b, err := readSource(root, file)
			if err != nil {
				return graph, err
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, b, parser.ParseComments)
			if err != nil {
				return graph, err
			}
			if name := packageNames[dir]; name != "" && name != f.Name.Name {
				return graph, fmt.Errorf("%s: files declare different packages", c.ID)
			}
			packageNames[dir] = f.Name.Name
			for _, comments := range f.Comments {
				for _, comment := range comments.List {
					if strings.HasPrefix(comment.Text, "//go:build") || strings.HasPrefix(comment.Text, "// +build") {
						return graph, fmt.Errorf("%s: platform/build-constrained proof files are not supported", file)
					}
				}
			}
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return graph, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			path := filepath.ToSlash(filepath.Join(dir, name))
			if strings.HasSuffix(name, ".gobra") {
				return graph, fmt.Errorf("%s: .gobra headers could shadow production Go; remove %s", c.ID, path)
			}
			if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && !strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "_") && !listed[path] {
				return graph, fmt.Errorf("%s: missing production package file %s", c.ID, path)
			}
		}
		// A nested module changes Go's import resolution but not this Gobra adapter's.
		for ancestor := filepath.Join(root, filepath.FromSlash(dir)); ancestor != root; ancestor = filepath.Dir(ancestor) {
			if _, err := os.Stat(filepath.Join(ancestor, "go.mod")); err == nil {
				return graph, fmt.Errorf("%s: nested Go modules are not supported", c.ID)
			}
			if filepath.Dir(ancestor) == ancestor {
				break
			}
		}
	}
	var hasImports bool
	imports := map[string][]string{}
	for dir, c := range byDir {
		for _, file := range c.Files {
			b, err := readSource(root, file)
			if err != nil {
				return graph, err
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, b, parser.ImportsOnly)
			if err != nil {
				return graph, err
			}
			for _, spec := range f.Imports {
				if spec.Name != nil && (spec.Name.Name == "." || spec.Name.Name == "_") {
					return graph, fmt.Errorf("%s: dot and side-effect imports are not supported", file)
				}
				name, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return graph, err
				}
				imports[dir] = append(imports[dir], name)
				hasImports = true
			}
		}
	}
	if hasImports {
		module, err := mainModule(root)
		if err != nil {
			return graph, err
		}
		graph.Module = module
	}
	for dir, c := range byDir {
		graph.Dependencies[c.ID] = []string{}
		for _, name := range imports[dir] {
			rel := "."
			if name != graph.Module {
				if !strings.HasPrefix(name, graph.Module+"/") {
					return graph, fmt.Errorf("%s: external import %q has no verified local implementation; external/standard-library proof imports are not supported", c.ID, name)
				}
				rel = strings.TrimPrefix(name, graph.Module+"/")
			}
			if _, err := Local(root, rel); rel != "." && err != nil {
				return graph, err
			}
			imported, ok := byDir[rel]
			if !ok {
				return graph, fmt.Errorf("%s: imported package %q needs its own complete Gobra check", c.ID, name)
			}
			if !contains(graph.Dependencies[c.ID], imported.ID) {
				graph.Dependencies[c.ID] = append(graph.Dependencies[c.ID], imported.ID)
			}
		}
		sort.Strings(graph.Dependencies[c.ID])
	}
	byID := map[string]Check{}
	for _, c := range p.Checks {
		byID[c.ID] = c
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if done[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("cyclic proof-package imports at %s", id)
		}
		visiting[id] = true
		for _, dep := range graph.Dependencies[id] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		done[id] = true
		graph.Order = append(graph.Order, byID[id])
		return nil
	}
	// Preserve authored order where dependencies do not constrain it.
	for _, c := range p.Checks {
		if err := visit(c.ID); err != nil {
			return graph, err
		}
	}
	return graph, nil
}

// mainModule reads just the module identity without running project tooling.
// Replacements and nested module requirements are rejected rather than letting
// Go and Gobra resolve the same import to different implementations.
func mainModule(root string) (string, error) {
	path := filepath.Join(root, "go.mod")
	if err := NoSymlinks(path); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("local proof imports require a root go.mod: %w", err)
	}
	var module string
	var tokens []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "replace" {
			return "", fmt.Errorf("proof imports do not support go.mod replace directives")
		}
		tokens = append(tokens, fields...)
		if fields[0] == "module" {
			if module != "" || len(fields) != 2 {
				return "", fmt.Errorf("invalid module directive in go.mod")
			}
			module = fields[1]
			if strings.HasPrefix(module, "\"") {
				module, err = strconv.Unquote(module)
				if err != nil {
					return "", err
				}
			}
		}
	}
	if module == "" || strings.ContainsAny(module, " \t\\\x00") || strings.HasPrefix(module, ".") || strings.HasSuffix(module, "/") {
		return "", fmt.Errorf("go.mod requires a supported module path")
	}
	// Apart from the module directive itself, a reference to a child module can
	// select a different implementation via Go's longest-module-path resolution.
	moduleSkipped := false
	for _, word := range tokens {
		word = strings.Trim(word, "\"()")
		if word == module && !moduleSkipped {
			moduleSkipped = true
			continue
		}
		if word == module || strings.HasPrefix(word, module+"/") {
			return "", fmt.Errorf("requirements on the main module or nested modules are not supported in proof imports")
		}
	}
	return module, nil
}

// symbolName uses Go's receiver-qualified spelling; pointer/value receivers use
// the same Type.Method identity, as they do for Go method declarations.
func symbolName(fn *ast.FuncDecl) (string, error) {
	if fn.Recv == nil {
		return fn.Name.Name, nil
	}
	if len(fn.Recv.List) != 1 {
		return "", fmt.Errorf("invalid method receiver")
	}
	typ := fn.Recv.List[0].Type
	if ptr, ok := typ.(*ast.StarExpr); ok {
		typ = ptr.X
	}
	id, ok := typ.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("generic or non-named method receivers are not supported")
	}
	return id.Name + "." + fn.Name.Name, nil
}

// VerificationOrder exposes the dependency-aware execution plan for review.
func (p Plan) VerificationOrder(root string) ([]Check, map[string][]string, error) {
	graph, err := resolvePackages(root, p)
	return graph.Order, graph.Dependencies, err
}
