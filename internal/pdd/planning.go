package pdd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DecisionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Consequence string `json:"consequence"`
	Recommended bool   `json:"recommended,omitempty"`
}
type Decision struct {
	ID        string           `json:"id"`
	Question  string           `json:"question"`
	Context   string           `json:"context"`
	File      string           `json:"file,omitempty"`
	Function  string           `json:"function,omitempty"`
	Options   []DecisionOption `json:"options"`
	Selected  string           `json:"selected,omitempty"`
	Rationale string           `json:"rationale,omitempty"`
}
type IntentItem struct {
	ID           string   `json:"id"`
	UserWords    string   `json:"user_words"`
	Requirements []string `json:"requirements"`
	Decisions    []string `json:"decisions,omitempty"`
}
type Scenario struct {
	ID           string   `json:"id"`
	Check        string   `json:"check"`
	Test         string   `json:"test"`
	Given        string   `json:"given"`
	When         string   `json:"when"`
	Then         string   `json:"then"`
	Requirements []string `json:"requirements,omitempty"`
	Purpose      string   `json:"purpose"` // boundary, integration, runtime, formal-regression
}
type AssumedClause struct {
	Impact string `json:"impact"`
	ID     string `json:"id"`
	Text   string `json:"text"`
	Test   string `json:"test"`
}
type WorkflowPolicy struct {
	MutableProofHints bool `json:"allow_proof_hints,omitempty"`
	DualReview        bool `json:"require_dual_review,omitempty"`
	GapReview         bool `json:"require_gap_review,omitempty"`
}

func validatePlanning(root string, p Plan, ids map[string]bool) error {
	add := func(id string) error {
		if !idRE.MatchString(id) || ids[id] {
			return fmt.Errorf("invalid or duplicate planning id %q", id)
		}
		ids[id] = true
		return nil
	}
	if p.Workflow != nil && p.Workflow.DualReview {
		if len(p.IntentMap) == 0 {
			return fmt.Errorf("dual-review workflow requires original-intent mappings")
		}
		for _, t := range p.Trust {
			if len(t.Clauses) == 0 || len(t.Affects) == 0 {
				return fmt.Errorf("dual-review workflow requires per-clause tests and affected requirements for trust %s", t.ID)
			}
		}
	}
	checks := map[string]Check{}
	reqs := map[string]bool{}
	decisions := map[string]bool{}
	for _, c := range p.Checks {
		checks[c.ID] = c
	}
	for _, r := range p.Requirements {
		reqs[r.ID] = true
	}
	for _, d := range p.Decisions {
		if err := add(d.ID); err != nil {
			return err
		}
		decisions[d.ID] = true
		if strings.TrimSpace(d.Question) == "" || strings.TrimSpace(d.Context) == "" || len(d.Options) < 2 {
			return fmt.Errorf("decision %s requires a question, context and at least two options", d.ID)
		}
		choices := map[string]bool{}
		recommended := 0
		for _, o := range d.Options {
			if !idRE.MatchString(o.ID) || choices[o.ID] || strings.TrimSpace(o.Label) == "" || strings.TrimSpace(o.Consequence) == "" {
				return fmt.Errorf("decision %s has an invalid option", d.ID)
			}
			choices[o.ID] = true
			if o.Recommended {
				recommended++
			}
		}
		if recommended > 1 {
			return fmt.Errorf("decision %s has multiple recommended options", d.ID)
		}
		if !choices[d.Selected] || strings.TrimSpace(d.Rationale) == "" {
			return fmt.Errorf("decision %s is unresolved; choose an option and record why before approval", d.ID)
		}
		if d.Function != "" && d.File == "" {
			return fmt.Errorf("decision %s needs a source file", d.ID)
		}
		if d.File != "" {
			data, err := readSource(root, d.File)
			if err != nil {
				return err
			}
			if d.Function != "" {
				if _, err = function(d.File, data, d.Function); err != nil {
					return err
				}
			}
		}
	}
	covered := map[string]bool{}
	for _, item := range p.IntentMap {
		if err := add(item.ID); err != nil {
			return err
		}
		if strings.TrimSpace(item.UserWords) == "" || len(item.Requirements) == 0 {
			return fmt.Errorf("intent %s requires the user's words and mapped requirements", item.ID)
		}
		if p.OriginalRequest != "" && !strings.Contains(p.OriginalRequest, item.UserWords) {
			return fmt.Errorf("intent %s must quote the original request verbatim", item.ID)
		}
		for _, id := range item.Requirements {
			if !reqs[id] {
				return fmt.Errorf("intent %s references unknown requirement %s", item.ID, id)
			}
			covered[id] = true
		}
		for _, id := range item.Decisions {
			if !decisions[id] {
				return fmt.Errorf("intent %s references unknown decision %s", item.ID, id)
			}
		}
	}
	if len(p.IntentMap) > 0 {
		for id := range reqs {
			if !covered[id] {
				return fmt.Errorf("requirement %s has no original-intent mapping", id)
			}
		}
	}
	for _, s := range p.Scenarios {
		if err := add(s.ID); err != nil {
			return err
		}
		c := checks[s.Check]
		if c.Kind != "go-test" || !contains(c.Tests, s.Test) || strings.TrimSpace(s.Given) == "" || strings.TrimSpace(s.When) == "" || strings.TrimSpace(s.Then) == "" {
			return fmt.Errorf("scenario %s needs a named empirical test and Given/When/Then", s.ID)
		}
		if s.Purpose != "boundary" && s.Purpose != "integration" && s.Purpose != "runtime" && s.Purpose != "formal-regression" {
			return fmt.Errorf("scenario %s has unknown purpose", s.ID)
		}
		for _, id := range s.Requirements {
			if !reqs[id] {
				return fmt.Errorf("scenario %s references unknown requirement %s", s.ID, id)
			}
		}
	}
	for _, t := range p.Trust {
		for _, id := range t.Affects {
			if !reqs[id] {
				return fmt.Errorf("trust %s affects unknown requirement %s", t.ID, id)
			}
		}
		for _, clause := range t.Clauses {
			if err := add(clause.ID); err != nil {
				return err
			}
			if strings.TrimSpace(clause.Text) == "" || strings.TrimSpace(clause.Impact) == "" || !contains(checks[t.Check].Tests, clause.Test) {
				return fmt.Errorf("trusted clause %s needs an assumption, impact if false, and a named test in %s", clause.ID, t.Check)
			}
			scenario := false
			for _, s := range p.Scenarios {
				if s.Check == t.Check && s.Test == clause.Test {
					scenario = true
				}
			}
			if !scenario {
				return fmt.Errorf("trusted clause %s requires a Given/When/Then scenario", clause.ID)
			}
		}
	}
	return nil
}

func LoadDraft(root, name string) (Plan, error) {
	paths, err := PathsFor(name)
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	if err = ReadJSON(filepath.Join(root, paths.File), &p); err != nil {
		return p, err
	}
	if p.Schema != 1 {
		return p, fmt.Errorf("only plan schema 1 is supported")
	}
	return p.Named(paths.Name)
}
func draftDigest(p Plan) string { data, _ := json.Marshal(p); return hashBytes(data) }
func AnswerDecision(root, name, id, option, reason, expected string) (Plan, error) {
	unlock, err := lockProject(root)
	if err != nil {
		return Plan{}, err
	}
	defer unlock()
	p, err := LoadDraft(root, name)
	if err != nil {
		return p, err
	}
	if expected != "" && expected != draftDigest(p) {
		return p, fmt.Errorf("draft changed since review; reload decisions before answering")
	}
	if strings.TrimSpace(reason) == "" {
		return p, fmt.Errorf("record the reason for the decision")
	}
	for i, d := range p.Decisions {
		if d.ID == id {
			valid := false
			for _, o := range d.Options {
				valid = valid || o.ID == option
			}
			if !valid {
				return p, fmt.Errorf("unknown option %s", option)
			}
			p.Decisions[i].Selected = option
			p.Decisions[i].Rationale = reason
			return p, WriteJSON(filepath.Join(root, p.Paths().File), p)
		}
	}
	return p, fmt.Errorf("unknown decision %s", id)
}

type PlanningView struct {
	PlanID          string   `json:"plan_id"`
	Digest          string   `json:"draft_digest"`
	Plan            Plan     `json:"plan"`
	ValidationError string   `json:"validation_error,omitempty"`
	OpenDecisions   []string `json:"open_decisions"`
}

func InspectDraft(root, name string) (PlanningView, error) {
	p, err := LoadDraft(root, name)
	if err != nil {
		return PlanningView{}, err
	}
	view := PlanningView{PlanID: p.ID(), Digest: draftDigest(p), Plan: p, OpenDecisions: []string{}}
	for _, d := range p.Decisions {
		if d.Selected == "" || strings.TrimSpace(d.Rationale) == "" {
			view.OpenDecisions = append(view.OpenDecisions, d.ID)
		}
	}
	if err = p.Validate(root); err != nil {
		view.ValidationError = err.Error()
	}
	return view, nil
}

// Installation creates an unapproved draft only. It never imports an agent's
// approval record or changes a saved approval to fit the generated intent.
func InstallDraft(root, name string, p Plan, expected string) (PlanningView, error) {
	unlock, err := lockProject(root)
	if err != nil {
		return PlanningView{}, err
	}
	defer unlock()
	paths, err := PathsFor(name)
	if err != nil {
		return PlanningView{}, err
	}
	current, err := LoadDraft(root, name)
	if err == nil {
		if expected == "" || expected != draftDigest(current) {
			return PlanningView{}, fmt.Errorf("draft exists; provide its inspected draft digest before replacement")
		}
	} else if !os.IsNotExist(err) {
		return PlanningView{}, err
	}
	p, err = p.Named(name)
	if err != nil {
		return PlanningView{}, err
	}
	if p.Schema != 1 {
		return PlanningView{}, fmt.Errorf("agent draft must use plan schema 1")
	}
	if err = WriteJSON(filepath.Join(root, paths.File), p); err != nil {
		return PlanningView{}, err
	}
	return InspectDraft(root, name)
}
