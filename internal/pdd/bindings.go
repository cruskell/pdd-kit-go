package pdd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

type AnnotationBinding struct {
	Owner      string `json:"owner"`
	Annotation string `json:"annotation"`
}

// Preconditions must be bound to the declaration they constrain. An ordered
// list of annotation text alone would allow moving `requires` from one helper
// to the next function without changing the approved digest. Absolute source
// positions are intentionally avoided so ordinary body edits remain possible.
func annotationBindings(file string, source []byte) ([]AnnotationBinding, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	bindings := []AnnotationBinding{}
	var functions []*ast.FuncDecl
	docOwners := map[token.Pos]string{}
	for _, decl := range parsed.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			functions = append(functions, fn)
			name, err := symbolName(fn)
			if err != nil {
				return nil, err
			}
			if fn.Doc != nil {
				for _, comment := range fn.Doc.List {
					docOwners[comment.Pos()] = "function:" + name + "/contract"
				}
			}
		}
	}
	commentMap := ast.NewCommentMap(fset, parsed, parsed.Comments)
	anchors := map[ast.Node]string{}
	ast.Walk(bindingVisitor{anchors: anchors}, parsed)
	mapped := map[*ast.CommentGroup]string{}
	for node, groups := range commentMap {
		for _, group := range groups {
			mapped[group] = anchors[node]
		}
	}
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			value := annotation(comment.Text)
			if value == "" {
				continue
			}
			owner, attached := docOwners[comment.Pos()]
			// Requirement mappings already bind function postconditions to their owner;
			// omit that redundant data to retain default-plan digest compatibility.
			if attached && strings.HasPrefix(value, "ensures ") {
				continue
			}
			if !attached {
				for _, fn := range functions {
					name, _ := symbolName(fn)
					if fn.Body != nil && comment.Pos() > fn.Body.Pos() && comment.End() < fn.Body.End() {
						owner = "function:" + name + "/body"
						break
					}
					if fn.Body != nil && comment.Pos() > fn.Pos() && comment.End() < fn.Body.Pos() {
						owner = "function:" + name + "/signature"
						break
					}
				}
			}
			if owner == "" {
				// Gobra ignores whitespace between a contract and its following function;
				// detached comments therefore still have a semantic owner, even when Go's
				// documentation-comment association omits them.
				for _, decl := range parsed.Decls {
					if comment.Pos() >= decl.Pos() && comment.End() <= decl.End() {
						break
					}
					if decl.Pos() > comment.End() {
						if fn, ok := decl.(*ast.FuncDecl); ok {
							name, _ := symbolName(fn)
							owner = "function:" + name + "/contract"
						}
						break
					}
				}
			}
			if owner == "" {
				owner = mapped[group]
			}
			if owner == "" {
				owner = "package:" + parsed.Name.Name
			}
			bindings = append(bindings, AnnotationBinding{owner, value})
		}
	}
	return bindings, nil
}

type bindingVisitor struct {
	parts   []string
	anchors map[ast.Node]string
}

func (v bindingVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		return nil
	}
	if _, ok := node.(*ast.CommentGroup); ok {
		return nil
	}
	if _, ok := node.(*ast.BlockStmt); ok {
		return nil
	} // body annotations use their containing function
	var part string
	switch n := node.(type) {
	case *ast.File:
		part = "package:" + n.Name.Name
	case *ast.FuncDecl:
		name, _ := symbolName(n)
		part = "function:" + name
	case *ast.GenDecl:
		part = n.Tok.String()
	case *ast.TypeSpec:
		part = "type:" + n.Name.Name
	case *ast.ValueSpec:
		var names []string
		for _, name := range n.Names {
			names = append(names, name.Name)
		}
		part = "values:" + strings.Join(names, ",")
	case *ast.Field:
		var names []string
		for _, name := range n.Names {
			names = append(names, name.Name)
		}
		if len(names) > 0 {
			part = "field:" + strings.Join(names, ",")
		}
	}
	if part != "" {
		v.parts = append(append([]string{}, v.parts...), part)
	}
	v.anchors[node] = strings.Join(v.parts, "/")
	return v
}
