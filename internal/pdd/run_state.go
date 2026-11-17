package pdd

import (
	"fmt"
	"path/filepath"
	"time"
)

const runStatePath = ".pdd/run.json"

type RunState struct {
	PlanID     string    `json:"plan_id,omitempty"`
	Schema     int       `json:"schema"`
	ID         string    `json:"id"`
	State      string    `json:"state"` // running, completed, interrupted
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Detail     string    `json:"detail,omitempty"`
}

func readRunState(root string, plans ...Plan) (RunState, error) {
	var p Plan
	if len(plans) > 0 {
		p = plans[0]
	}
	var state RunState
	err := ReadJSON(filepath.Join(root, p.stateFile("run.json")), &state)
	if err != nil {
		return state, err
	}
	if state.Schema != 1 || !matchesPlanID(state.PlanID, p) || state.ID == "" || filepath.Base(state.ID) != state.ID || state.StartedAt.IsZero() {
		return state, fmt.Errorf("invalid latest-run record")
	}
	if state.State != "running" && state.State != "completed" && state.State != "interrupted" {
		return state, fmt.Errorf("invalid latest-run state")
	}
	if state.ID != state.StartedAt.Format("20060102T150405.000000000Z") {
		return state, fmt.Errorf("latest-run identity does not match its timestamp")
	}
	if state.State != "running" && state.FinishedAt.IsZero() {
		return state, fmt.Errorf("latest-run finish timestamp is missing")
	}
	return state, nil
}
