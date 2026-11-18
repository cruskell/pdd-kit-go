package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cruskell/proofsteer-go/internal/pdd"
)

func renderReview(out io.Writer, report pdd.ReviewReport, diff bool) {
	plan := report.Bundle.Plan
	fmt.Fprintf(out, "# %s [%s]\n\n%s\n\nDigest: %s\n\n", plan.Name, report.Bundle.PlanID, plan.Intent, report.Digest)
	for _, requirement := range plan.Requirements {
		fmt.Fprintf(out, "[%s] %s: %s\n  check: %s\n", requirement.Kind, requirement.ID, requirement.Text, requirement.Check)
		if requirement.Kind == "formal" {
			fmt.Fprintf(out, "  %s :: %s\n  ensures %s\n", requirement.File, requirement.Function, requirement.Ensures)
		}
	}
	fmt.Fprintln(out, "\nChecks in verification order:")
	for _, check := range report.Order {
		fmt.Fprintf(out, "  %s (%s)\n", check.ID, check.Kind)
		if check.Kind == "gobra" {
			fmt.Fprintln(out, "    files:", strings.Join(check.Files, ", "))
		} else {
			fmt.Fprintf(out, "    package: %s; tests: %s\n", check.Package, strings.Join(check.Tests, ", "))
		}
		if len(report.Dependencies[check.ID]) > 0 {
			fmt.Fprintln(out, "    requires verified:", strings.Join(report.Dependencies[check.ID], ", "))
		}
	}
	fmt.Fprintln(out, "\nTrusted boundary (requires explicit acceptance):")
	for _, trust := range plan.Trust {
		fmt.Fprintf(out, "  %s: %s\n  empirical check: %s\n", trust.ID, trust.Assumption, trust.Check)
	}
	fmt.Fprintln(out, "\nOutside the guarantee:")
	for _, limit := range plan.NonGoals {
		fmt.Fprintln(out, " -", limit)
	}
	fmt.Fprintln(out, "\nInput scope: default 16 MiB/file, 128 MiB total, 20000 files; excludes root .git, .pdd, bin, dist.")
	if plan.Inputs != nil {
		policy, _ := json.Marshal(plan.Inputs)
		fmt.Fprintln(out, "  Reviewed overrides (zero limits use defaults):", string(policy))
	}
	fmt.Fprintln(out, "\nMutations:")
	for _, mutation := range plan.Mutations {
		fmt.Fprintf(out, "  %s -> %s: %q => %q\n", mutation.ID, mutation.Requirement, mutation.Find, mutation.Replace)
	}
	fmt.Fprintln(out, "\nFrozen contracts and declarations:")
	for _, source := range report.Bundle.Sources {
		fmt.Fprintln(out, " ", source.File)
		for _, item := range source.Annotations {
			if strings.HasPrefix(item, "declarations:") {
				fmt.Fprintln(out, strings.TrimPrefix(item, "declarations:"))
			} else {
				fmt.Fprintln(out, "    // @ "+item)
			}
		}
		for _, binding := range source.Bindings {
			fmt.Fprintf(out, "    binding: %s -> %s\n", binding.Owner, binding.Annotation)
		}
		if len(source.Definitions) > 0 {
			fmt.Fprintln(out, "    frozen pure definitions:", string(source.Definitions))
		}
	}
	fmt.Fprintf(out, "\nApproval: %s\n", report.Approval.State)
	if report.Approval.Message != "" {
		fmt.Fprintln(out, report.Approval.Message)
	}
	if diff {
		fmt.Fprintln(out, "\nChanges since recorded review:")
		if !report.ChangesAvailable {
			fmt.Fprintln(out, "  Previous review content was not recorded; review the complete current plan and contracts.")
		} else if len(report.Changes) == 0 {
			fmt.Fprintln(out, "  No approved intent or contract changes.")
		} else {
			for _, change := range report.Changes {
				before, _ := json.Marshal(change.Before)
				after, _ := json.Marshal(change.After)
				fmt.Fprintf(out, "  %s\n    before: %s\n    after:  %s\n", change.Path, before, after)
			}
		}
	}
}
