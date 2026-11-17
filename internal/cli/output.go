package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

type CommandError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}
type Response struct {
	Schema   int           `json:"schema"`
	Command  string        `json:"command"`
	Plan     string        `json:"plan"`
	OK       bool          `json:"ok"`
	ExitCode int           `json:"exit_code"`
	Data     interface{}   `json:"data,omitempty"`
	Error    *CommandError `json:"error,omitempty"`
}
type output struct {
	json             bool
	out, diagnostics io.Writer
	command, plan    string
}

func (o output) finish(code int, data interface{}, kind string, err error, hint string) int {
	response := Response{Schema: 1, Command: o.command, Plan: o.plan, OK: code == 0, ExitCode: code, Data: data}
	if err != nil {
		response.Error = &CommandError{kind, err.Error(), hint}
	}
	if o.json {
		if writeErr := json.NewEncoder(o.out).Encode(response); writeErr != nil {
			fmt.Fprintln(o.diagnostics, "error writing JSON output:", writeErr)
			return 1
		}
	} else if err != nil {
		fmt.Fprintln(o.diagnostics, "error:", err)
		if hint != "" {
			fmt.Fprintln(o.diagnostics, hint)
		}
	}
	return code
}

type checkedWriter struct {
	writer io.Writer
	err    error
}

func (w *checkedWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n != len(data) && err == nil {
		err = io.ErrShortWrite
	}
	if err != nil && w.err == nil {
		w.err = err
	}
	return n, err
}
