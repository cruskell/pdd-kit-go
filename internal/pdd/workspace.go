package pdd

import (
	"fmt"
	"path/filepath"
)

type PlanPaths struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	State string `json:"state"`
}

// Default paths stay compatible with the first releases. Named plans have an
// independent state namespace, while all plans share the project execution lock.
func PathsFor(name string) (PlanPaths, error) {
	if name == "" || name == "default" {
		return PlanPaths{"default", PlanPath, ".pdd"}, nil
	}
	if !idRE.MatchString(name) {
		return PlanPaths{}, fmt.Errorf("plan name must be default or lowercase letters, digits and hyphens starting with a letter")
	}
	return PlanPaths{name, filepath.ToSlash(filepath.Join(".pdd", "plans", name+".json")), filepath.ToSlash(filepath.Join(".pdd", "state", name))}, nil
}
func (p Plan) Named(name string) (Plan, error) {
	paths, err := PathsFor(name)
	if err != nil {
		return p, err
	}
	p.selectedName = paths.Name
	return p, nil
}
func (p Plan) Paths() PlanPaths             { paths, _ := PathsFor(p.selectedName); return paths }
func (p Plan) ID() string                   { return p.Paths().Name }
func (p Plan) stateFile(name string) string { return filepath.Join(p.Paths().State, name) }

func LoadNamed(root, name string) (Plan, error) {
	paths, err := PathsFor(name)
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	if err = ReadJSON(filepath.Join(root, paths.File), &p); err != nil {
		return p, err
	}
	p.selectedName = paths.Name
	return p, p.Validate(root)
}
func matchesPlanID(recordID string, p Plan) bool {
	return recordID == p.ID() || (recordID == "" && p.ID() == "default")
}
