package pdd

import (
	"fmt"
	"os"
	"path/filepath"
)

type Initialization struct {
	Path            string `json:"path"`
	Draft           bool   `json:"draft"`
	Source          string `json:"source,omitempty"`
	ValidationError string `json:"validation_error,omitempty"`
}

// Initialize imports intent only: it never executes checks or records approval.
// The project lock makes two pdd init invocations unable to overwrite each other.
func Initialize(root, name, source string) (Initialization, error) {
	paths, err := PathsFor(name)
	if err != nil {
		return Initialization{}, err
	}
	unlock, err := lockProject(root)
	if err != nil {
		return Initialization{}, err
	}
	defer unlock()
	target := filepath.Join(root, paths.File)
	if err = NoSymlinks(target); err != nil {
		return Initialization{}, err
	}
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return Initialization{}, fmt.Errorf("%s already exists or cannot be accessed", target)
	}
	plan := Plan{Schema: 1, Name: filepath.Base(root), Intent: "Describe the behavior to prove, including refusal and success cases.", NonGoals: []string{"List behavior and external dependencies outside this proof."}, Requirements: []Requirement{}, Checks: []Check{}, Trust: []Trust{}, Mutations: []Mutation{}}
	result := Initialization{Path: target, Draft: true}
	if source != "" {
		if !filepath.IsAbs(source) {
			source = filepath.Join(root, source)
		}
		if err = ReadJSON(source, &plan); err != nil {
			return result, err
		}
		if plan.Schema != 1 {
			return result, fmt.Errorf("only plan schema 1 can be imported")
		}
		result.Source = source
	}
	plan, err = plan.Named(paths.Name)
	if err != nil {
		return result, err
	}
	if err = plan.Validate(root); err != nil {
		result.ValidationError = err.Error()
	}
	if err = WriteJSON(target, plan); err != nil {
		return result, err
	}
	return result, nil
}
