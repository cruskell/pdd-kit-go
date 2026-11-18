package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	pddkit "github.com/cruskell/pdd-kit-go"
	"github.com/cruskell/pdd-kit-go/internal/pdd"
)

type demoResult struct {
	Directory string `json:"directory"`
	Report    string `json:"report,omitempty"`
	Receipt   string `json:"receipt,omitempty"`
}

func runDemo(ctx context.Context, printer output, destination string, options pdd.Options) int {
	result := demoResult{Directory: destination}
	fail := func(err error) int { return printer.finish(1, result, "demo", err, "") }
	if err := pdd.NoSymlinks(destination); err != nil {
		return fail(err)
	}
	if err := os.Mkdir(destination, 0755); err != nil {
		return fail(fmt.Errorf("create a new demo directory: %w", err))
	}
	files := pddkit.DemoFiles()
	if err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		return pdd.AtomicWrite(filepath.Join(destination, filepath.FromSlash(name)), data)
	}); err != nil {
		return fail(err)
	}
	plan, err := pdd.Load(destination)
	if err != nil {
		return fail(err)
	}
	if _, err := pdd.ImportApproval(destination, plan, ".pdd/reviews/default.json"); err != nil {
		return fail(err)
	}
	receipt, dir, err := pdd.Prove(ctx, destination, plan, options)
	if err != nil {
		return fail(err)
	}
	result.Receipt = filepath.Join(dir, "receipt.md")
	report := filepath.Join(destination, ".pdd", "results", "demo.html")
	if err := pdd.WriteDashboard(ctx, destination, plan.ID(), "", report); err != nil {
		return fail(err)
	}
	result.Report = report
	if !printer.json {
		fmt.Fprintln(printer.out, "Report:", result.Report)
		fmt.Fprintln(printer.out, "Receipt:", result.Receipt)
	}
	if !receipt.Success {
		return fail(fmt.Errorf("demo verification did not pass; inspect the report"))
	}
	_, latest, err := pdd.StatusContext(ctx, destination, plan, "")
	if err != nil {
		return fail(err)
	}
	if latest != dir {
		return fail(fmt.Errorf("a newer run replaced the demo receipt"))
	}
	return printer.finish(0, result, "", nil, "")
}
