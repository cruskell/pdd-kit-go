package pdd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type FrozenSource struct {
	File        string              `json:"file"`
	Annotations []string            `json:"annotations"`
	Definitions json.RawMessage     `json:"definitions,omitempty"`
	Bindings    []AnnotationBinding `json:"bindings,omitempty"`
}
type ReviewBundle struct {
	Schema  int            `json:"schema"`
	PlanID  string         `json:"plan_id"`
	Plan    Plan           `json:"plan"`
	Module  string         `json:"module,omitempty"`
	Sources []FrozenSource `json:"sources"`
}
type ReviewChange struct {
	Path   string      `json:"path"`
	Before interface{} `json:"before"`
	After  interface{} `json:"after"`
}
type ApprovalState struct {
	State      string     `json:"state"`
	Message    string     `json:"message,omitempty"`
	Digest     string     `json:"digest,omitempty"`
	Reviewer   string     `json:"reviewer,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
}
type ReviewReport struct {
	Digest           string              `json:"digest"`
	Bundle           ReviewBundle        `json:"bundle"`
	Order            []Check             `json:"verification_order"`
	Dependencies     map[string][]string `json:"dependencies"`
	Approval         ApprovalState       `json:"approval"`
	ChangesAvailable bool                `json:"changes_available"`
	Changes          []ReviewChange      `json:"changes"`
}

func BuildReview(root string, p Plan) (ReviewBundle, error) { return buildBaseReview(root, p) }
func specificationDigest(root string, p Plan) (string, error) {
	p.PlanningReviews = nil
	r, err := buildBaseReview(root, p)
	if err != nil {
		return "", err
	}
	return r.digest()
}
func buildBaseReview(root string, p Plan) (ReviewBundle, error) {
	graph, err := resolvePackages(root, p)
	if err != nil {
		return ReviewBundle{}, err
	}
	review := ReviewBundle{Schema: 1, PlanID: p.ID(), Plan: p, Module: graph.Module, Sources: []FrozenSource{}}
	files := map[string]bool{}
	for _, check := range p.Checks {
		if check.Kind == "gobra" {
			for _, file := range check.Files {
				files[file] = true
			}
		}
	}
	var names []string
	for file := range files {
		names = append(names, file)
	}
	sort.Strings(names)
	for _, file := range names {
		source, err := readSource(root, file)
		if err != nil {
			return review, err
		}
		annotations, err := reviewedAnnotations(file, source, p.Workflow != nil && p.Workflow.MutableProofHints)
		if err != nil {
			return review, err
		}
		definitions, err := specificationDefinitions(file, source)
		if err != nil {
			return review, err
		}
		bindings, err := reviewedBindings(file, source, p.Workflow != nil && p.Workflow.MutableProofHints)
		if err != nil {
			return review, err
		}
		review.Sources = append(review.Sources, FrozenSource{File: file, Annotations: annotations, Definitions: definitions, Bindings: bindings})
	}
	return review, nil
}

// digest preserves the default-plan hashing contract of previous versions, and
// additionally binds named plans to their independent identity. The stored
// bundle can be checked without executing or reading an old checkout.
func (r ReviewBundle) digest() (string, error) {
	if r.Schema != 1 {
		return "", fmt.Errorf("unsupported review bundle schema")
	}
	paths, err := PathsFor(r.PlanID)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(r.Plan)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	h.Write(data)
	if paths.Name != "default" {
		encoded, _ := json.Marshal(struct{ PlanID string }{paths.Name})
		h.Write(encoded)
	}
	if r.Module != "" {
		encoded, _ := json.Marshal(struct{ Module string }{r.Module})
		h.Write(encoded)
	}
	sources := append([]FrozenSource{}, r.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].File < sources[j].File })
	for _, source := range sources {
		encoded, _ := json.Marshal(struct {
			File        string
			Annotations []string
		}{source.File, source.Annotations})
		h.Write(encoded)
		if len(source.Bindings) > 0 {
			bound, _ := json.Marshal(struct{ Bindings []AnnotationBinding }{source.Bindings})
			h.Write(bound)
		}
		if len(source.Definitions) > 0 {
			var compact bytes.Buffer
			if err = json.Compact(&compact, source.Definitions); err != nil {
				return "", err
			}
			h.Write(compact.Bytes())
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
func Digest(root string, p Plan) (string, error) {
	review, err := BuildReview(root, p)
	if err != nil {
		return "", err
	}
	return review.digest()
}

func authoredPlanMatches(root string, p Plan) error {
	var authored Plan
	if err := ReadJSON(filepath.Join(root, p.Paths().File), &authored); err != nil {
		return err
	}
	expected, _ := json.Marshal(p)
	actual, _ := json.Marshal(authored)
	if !bytes.Equal(expected, actual) {
		return fmt.Errorf("plan changed since it was loaded; reload and review the selected plan")
	}
	return nil
}
func ReportReview(root string, p Plan) (ReviewReport, error) {
	bundle, err := BuildReview(root, p)
	if err != nil {
		return ReviewReport{}, err
	}
	digest, err := bundle.digest()
	if err != nil {
		return ReviewReport{}, err
	}
	order, deps, err := p.VerificationOrder(root)
	if err != nil {
		return ReviewReport{}, err
	}
	report := ReviewReport{Digest: digest, Bundle: bundle, Order: order, Dependencies: deps, Changes: []ReviewChange{}, Approval: ApprovalState{State: "missing"}}
	var approval Approval
	if err = ReadJSON(filepath.Join(root, p.stateFile("approval.json")), &approval); err != nil {
		report.Approval.Message = err.Error()
		if !os.IsNotExist(err) {
			report.Approval.State = "invalid"
		}
		return report, nil
	}
	report.Approval.Digest = approval.Digest
	report.Approval.Reviewer = approval.Reviewer
	report.Approval.ApprovedAt = &approval.ApprovedAt
	report.Approval.State = "current"
	if err = CheckApproval(root, p); err != nil {
		report.Approval.State = "stale"
		report.Approval.Message = err.Error()
	}
	if approval.Review == nil {
		return report, nil
	}
	stored, err := approval.Review.digest()
	if err != nil || stored != approval.Digest || !matchesPlanID(approval.Review.PlanID, p) {
		report.Approval.State = "invalid"
		report.Approval.Message = "saved review does not match approval digest or plan identity"
		return report, nil
	}
	report.ChangesAvailable = true
	diffReview("", reviewView(*approval.Review), reviewView(bundle), &report.Changes)
	return report, nil
}

// Resource arrays become ID-keyed objects so adding one requirement does not
// make every later row look changed. Ordinary lists remain ordered values.
func reviewView(bundle ReviewBundle) interface{} {
	data, _ := json.Marshal(bundle)
	var view map[string]interface{}
	_ = json.Unmarshal(data, &view)
	if plan, ok := view["plan"].(map[string]interface{}); ok {
		for _, key := range []string{"requirements", "checks", "trusted_boundary", "mutations"} {
			if items, ok := plan[key].([]interface{}); ok {
				byID := map[string]interface{}{}
				for _, item := range items {
					if object, ok := item.(map[string]interface{}); ok {
						if id, ok := object["id"].(string); ok {
							byID[id] = object
						}
					}
				}
				plan[key] = byID
			}
		}
	}
	if items, ok := view["sources"].([]interface{}); ok {
		byFile := map[string]interface{}{}
		for _, item := range items {
			if source, ok := item.(map[string]interface{}); ok {
				if file, ok := source["file"].(string); ok {
					byFile[file] = source
				}
			}
		}
		view["sources"] = byFile
	}
	return view
}
func diffReview(path string, before, after interface{}, changes *[]ReviewChange) {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if bytes.Equal(beforeJSON, afterJSON) {
		return
	}
	a, aOK := before.(map[string]interface{})
	b, bOK := after.(map[string]interface{})
	if aOK && bOK {
		keys := map[string]bool{}
		for key := range a {
			keys[key] = true
		}
		for key := range b {
			keys[key] = true
		}
		var ordered []string
		for key := range keys {
			ordered = append(ordered, key)
		}
		sort.Strings(ordered)
		for _, key := range ordered {
			next := key
			if path != "" {
				next = path + "." + key
			}
			diffReview(next, a[key], b[key], changes)
		}
		return
	}
	*changes = append(*changes, ReviewChange{path, before, after})
}
