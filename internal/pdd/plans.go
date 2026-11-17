package pdd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PlanSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	File  string `json:"file"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

func ListPlans(root string) ([]PlanSummary, error) {
	result := []PlanSummary{}
	add := func(name, path string) {
		summary := PlanSummary{ID: name, File: path}
		plan, err := LoadNamed(root, name)
		if err != nil {
			summary.Error = err.Error()
		} else {
			summary.Name = plan.Name
			summary.Valid = true
		}
		result = append(result, summary)
	}
	if _, err := os.Lstat(filepath.Join(root, PlanPath)); err == nil {
		add("default", PlanPath)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	dir := filepath.Join(root, ".pdd", "plans")
	if err := NoSymlinks(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		path := filepath.ToSlash(filepath.Join(".pdd", "plans", entry.Name()))
		paths, err := PathsFor(name)
		if err != nil || paths.Name == "default" {
			result = append(result, PlanSummary{ID: name, File: path, Error: "invalid or reserved named-plan filename"})
			continue
		}
		add(name, path)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
