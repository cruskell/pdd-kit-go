package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUsageAndDraftWorkflow(t *testing.T) {
	root := t.TempDir()
	var out, errOut bytes.Buffer
	run := func(args ...string) int {
		t.Helper()
		out.Reset()
		errOut.Reset()
		return Run(append([]string{"--cwd", root}, args...), &out, &errOut, "test")
	}
	if run("init") != 0 {
		t.Fatal(errOut.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".pdd/plan.json")); err != nil {
		t.Fatal(err)
	}
	if run("init") != 1 {
		t.Fatal("init overwrote existing plan")
	}
	if run("lint") != 1 {
		t.Fatal("unfinished scaffold passed lint")
	}
	if run("approve", "--reviewer", "someone") != 1 {
		t.Fatal("unfinished scaffold approved")
	}
	if run("prove") != 1 {
		t.Fatal("unfinished scaffold executed")
	}
	if run("unknown") != 2 {
		t.Fatal("unknown command exit code")
	}
	if run("lint", "--unknown") != 2 {
		t.Fatal("unknown option exit code")
	}
	if run("--help") != 0 || out.Len() == 0 {
		t.Fatal("missing help")
	}
	if run("--version") != 0 || out.String() != "test\n" {
		t.Fatal("version")
	}
}
