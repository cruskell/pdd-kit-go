package pdd

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
)

type InputPolicy struct {
	Exclude       []string `json:"exclude,omitempty"`
	MaxFileBytes  int64    `json:"max_file_bytes,omitempty"`
	MaxTotalBytes int64    `json:"max_total_bytes,omitempty"`
	MaxFiles      int      `json:"max_files,omitempty"`
}

func effectivePolicy(policy *InputPolicy) (InputPolicy, error) {
	value := InputPolicy{MaxFileBytes: 16 << 20, MaxTotalBytes: 128 << 20, MaxFiles: 20000}
	if policy != nil {
		value.Exclude = append([]string{}, policy.Exclude...)
		if policy.MaxFileBytes != 0 {
			value.MaxFileBytes = policy.MaxFileBytes
		}
		if policy.MaxTotalBytes != 0 {
			value.MaxTotalBytes = policy.MaxTotalBytes
		}
		if policy.MaxFiles != 0 {
			value.MaxFiles = policy.MaxFiles
		}
	}
	if value.MaxFileBytes < 1 || value.MaxFileBytes > 256<<20 || value.MaxTotalBytes < value.MaxFileBytes || value.MaxTotalBytes > 512<<20 || value.MaxFiles < 1 || value.MaxFiles > 100000 {
		return value, fmt.Errorf("inputs: use file limits 1..256 MiB, total >= file limit and <=512 MiB, files 1..100000")
	}
	seen := map[string]bool{}
	for _, path := range value.Exclude {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path == "" || path == "." || filepath.IsAbs(path) || strings.ContainsAny(path, "\\*?[]\x00") || clean != path || path == ".." || strings.HasPrefix(path, "../") || seen[path] {
			return value, fmt.Errorf("inputs.exclude: use distinct canonical relative paths without wildcards: %q", path)
		}
		seen[path] = true
	}
	return value, nil
}
func containsPath(parent, child string) bool {
	return parent == "." || parent == child || strings.HasPrefix(child, parent+"/")
}
func excludedPath(path string, policy InputPolicy) bool {
	first := strings.Split(path, "/")[0]
	if first == ".git" || first == ".pdd" || first == "bin" || first == "dist" {
		return true
	}
	for _, excluded := range policy.Exclude {
		if containsPath(excluded, path) {
			return true
		}
	}
	return false
}

type protectedInput struct {
	path string
	tree bool
}

func validateInputs(root string, p Plan) error {
	policy, err := effectivePolicy(p.Inputs)
	if err != nil {
		return err
	}
	for _, check := range p.Checks {
		if check.Kind != "playwright" {
			continue
		}
		for _, excluded := range policy.Exclude {
			if filepath.Base(excluded) != "node_modules" || containsPath(excluded, check.Config) {
				return fmt.Errorf("playwright inputs can exclude only provisioned node_modules outside the test configuration")
			}
		}
	}
	protected, err := protectedInputs(root, p)
	if err != nil {
		return err
	}
	for _, required := range protected {
		if excludedPath(required.path, policy) {
			return fmt.Errorf("inputs exclude required Go/build input %s", required.path)
		}
		if required.tree {
			for _, excluded := range policy.Exclude {
				if containsPath(required.path, excluded) {
					return fmt.Errorf("inputs exclude protected embedded/test input %s", excluded)
				}
			}
		}
	}
	return nil
}

// Determine local Go input closure without executing the project or fetching
// modules. External module implementations remain part of the empirical TCB.
func protectedInputs(root string, p Plan) ([]protectedInput, error) {
	result := []protectedInput{{"go.mod", false}, {"go.sum", false}}
	policy, err := effectivePolicy(p.Inputs)
	if err != nil {
		return nil, err
	}
	if err = NoSymlinks(filepath.Join(root, "go.mod")); err != nil {
		return nil, err
	}
	var parsedBytes int64
	parsedFiles := 0
	empirical := false
	for _, check := range p.Checks {
		empirical = empirical || check.Kind == "go-test"
	}
	if empirical {
		if _, err := os.Stat(filepath.Join(root, "vendor/modules.txt")); err == nil {
			result = append(result, protectedInput{"vendor", true})
		}
	}
	var module string
	if data, err := readArtifact(filepath.Join(root, "go.mod"), 8<<20); err == nil {
		if empirical {
			replacements, err := localReplacements(root, string(data))
			if err != nil {
				return nil, err
			}
			result = append(result, replacements...)
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(strings.SplitN(line, "//", 2)[0])
			if len(fields) == 2 && fields[0] == "module" {
				module = fields[1]
				if strings.HasPrefix(module, "\"") {
					module, _ = strconv.Unquote(module)
				}
				break
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	queue := []string{}
	for _, check := range p.Checks {
		if check.Kind == "gobra" {
			for _, file := range check.Files {
				queue = append(queue, filepath.ToSlash(filepath.Dir(file)))
			}
		} else if check.Kind == "go-test" {
			dir := strings.TrimPrefix(check.Package, "./")
			if dir == "" {
				dir = "."
			}
			queue = append(queue, dir)
		}
	}
	visited := map[string]bool{}
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		if visited[dir] {
			continue
		}
		visited[dir] = true
		absolute := filepath.Join(root, filepath.FromSlash(dir))
		if err := NoSymlinks(absolute); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(absolute)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			relative := filepath.ToSlash(filepath.Join(dir, entry.Name()))
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					result = append(result, protectedInput{relative, true})
				}
				continue
			}
			extension := filepath.Ext(entry.Name())
			switch extension {
			case ".go", ".gobra", ".s", ".S", ".c", ".C", ".h", ".H", ".cc", ".cpp", ".syso":
				result = append(result, protectedInput{relative, false})
			}
			if extension != ".go" || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			parsedFiles++
			parsedBytes += info.Size()
			if info.Size() > policy.MaxFileBytes || parsedBytes > policy.MaxTotalBytes || parsedFiles > policy.MaxFiles {
				return nil, fmt.Errorf("Go input closure exceeds reviewed input limits at %s", relative)
			}
			data, err := readSource(root, relative)
			if err != nil {
				return nil, err
			}
			file, err := parser.ParseFile(token.NewFileSet(), relative, data, parser.ParseComments)
			if err != nil {
				return nil, err
			}
			for _, imported := range file.Imports {
				name, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return nil, err
				}
				if module != "" && (name == module || strings.HasPrefix(name, module+"/")) {
					target := "."
					if name != module {
						target = strings.TrimPrefix(name, module+"/")
					}
					if target != "." {
						if _, err := Local(root, target); err != nil {
							return nil, err
						}
					}
					queue = append(queue, target)
				}
			}
			for _, group := range file.Comments {
				for _, comment := range group.List {
					if !strings.HasPrefix(comment.Text, "//go:embed ") {
						continue
					}
					patterns, err := embedPatterns(strings.TrimPrefix(comment.Text, "//go:embed "))
					if err != nil {
						return nil, fmt.Errorf("%s: %w", relative, err)
					}
					for _, pattern := range patterns {
						pattern = strings.TrimPrefix(pattern, "all:")
						for _, part := range strings.Split(pattern, "/") {
							if part == "." || part == ".." || part == "" {
								return nil, fmt.Errorf("%s: invalid embed pattern", relative)
							}
						}
						if filepath.IsAbs(pattern) || strings.Contains(pattern, "\\") || strings.HasPrefix(pattern, "../") {
							return nil, fmt.Errorf("%s: invalid embed pattern", relative)
						}
						matches, err := filepath.Glob(filepath.Join(absolute, filepath.FromSlash(pattern)))
						if err != nil {
							return nil, err
						}
						for _, match := range matches {
							rel, err := filepath.Rel(root, match)
							if err != nil {
								return nil, err
							}
							info, err := os.Lstat(match)
							if err != nil {
								return nil, err
							}
							result = append(result, protectedInput{filepath.ToSlash(rel), info.IsDir()})
						}
					}
				}
			}
		}
	}
	return result, nil
}
func embedPatterns(text string) ([]string, error) {
	var scan scanner.Scanner
	scan.Init(strings.NewReader(text))
	scan.Mode = scanner.ScanStrings | scanner.ScanRawStrings
	var parseErr error
	scan.Error = func(_ *scanner.Scanner, message string) { parseErr = fmt.Errorf("invalid embed patterns: %s", message) }
	var result []string
	for token := scan.Scan(); token != scanner.EOF; token = scan.Scan() {
		value := scan.TokenText()
		if token == scanner.String || token == scanner.RawString {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return nil, err
			}
			result = append(result, decoded)
		} else {
			// scanner tokenizes punctuation; gather an unquoted pattern up to whitespace.
			for next := scan.Peek(); next != scanner.EOF && next != ' ' && next != '\t' && next != '\r' && next != '\n'; next = scan.Peek() {
				value += string(scan.Next())
			}
			result = append(result, value)
		}
	}
	return result, parseErr
}

func localReplacements(root, mod string) ([]protectedInput, error) {
	result := []protectedInput{}
	inBlock := false
	for _, line := range strings.Split(mod, "\n") {
		text := strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		if text == "" {
			continue
		}
		fields, err := embedPatterns(text)
		if err != nil {
			return nil, err
		}
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "replace" {
			fields = fields[1:]
			if len(fields) > 0 && fields[0] == "(" {
				inBlock = true
				continue
			}
		} else if !inBlock {
			continue
		}
		if len(fields) == 1 && fields[0] == ")" {
			inBlock = false
			continue
		}
		arrow := -1
		for i, field := range fields {
			if field == "=>" {
				arrow = i
				break
			}
		}
		if arrow < 0 || arrow+1 >= len(fields) {
			return nil, fmt.Errorf("could not inspect local module replacements")
		}
		target := fields[arrow+1]
		local := strings.HasPrefix(target, ".") || filepath.IsAbs(target)
		if !local {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, filepath.FromSlash(target))
		}
		relative, err := filepath.Rel(root, target)
		if err != nil {
			return nil, err
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("local module replacement outside the project cannot be snapshotted: %s", target)
		}
		result = append(result, protectedInput{filepath.ToSlash(relative), true})
	}
	return result, nil
}
