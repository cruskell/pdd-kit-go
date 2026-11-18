package pdd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func mutableProofHint(text string) bool {
	for _, word := range []string{"assert", "invariant", "decreases", "fold", "unfold"} {
		if text == word || strings.HasPrefix(text, word+" ") {
			return true
		}
	}
	return false
}
func reviewedAnnotations(file string, data []byte, mutable bool) ([]string, error) {
	all, err := contracts(file, data)
	if err != nil || !mutable {
		return all, err
	}
	f, err := parser.ParseFile(token.NewFileSet(), file, data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var bodies []*ast.BlockStmt
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
			bodies = append(bodies, fn.Body)
		}
	}
	result := []string{}
	for _, group := range f.Comments {
		for _, c := range group.List {
			a := annotation(c.Text)
			if a == "" {
				continue
			}
			inBody := false
			for _, body := range bodies {
				inBody = inBody || (c.Pos() > body.Pos() && c.End() < body.End())
			}
			if !inBody || !mutableProofHint(a) {
				result = append(result, a)
			}
		}
	}
	for _, a := range all {
		if strings.HasPrefix(a, "declarations:") {
			result = append(result, a)
		}
	}
	return result, nil
}
func reviewedBindings(file string, data []byte, mutable bool) ([]AnnotationBinding, error) {
	all, err := annotationBindings(file, data)
	if err != nil || !mutable {
		return all, err
	}
	result := []AnnotationBinding{}
	for _, b := range all {
		if strings.HasSuffix(b.Owner, "/body") && mutableProofHint(b.Annotation) {
			continue
		}
		result = append(result, b)
	}
	return result, nil
}
