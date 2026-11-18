# Changelog

## v0.4.0

- Bundle the reviewed login example in `pdd demo` for first-time users.
- Show current check verdicts, negative examples and log links in HTML reports.
- Run named Playwright tests and record first-attempt results, screenshots and traces.
- Add a Chromium wallet example and a walkthrough for a separate Go module.

## v0.3.0

- Add request-driven planning, contextual decision cards and original-intent mappings.
- Require separate advisory intent/guarantee reviews for generated plans before human approval.
- Add explicit per-assumed-clause acceptance, impact and Given/When/Then checks.
- Add bounded scratch implementation/proof/gap-review loops with optional verified installation.
- Keep public contracts frozen while permitting explicitly enabled provable body hints.
- Add implementation-independent last-return negative mutations, task/coverage views,
  offline HTML review and explicitly requested evidence-backed draft PR creation.
- Preserve legacy plans/reviews and document remaining reference/backend differences.
- Validate the repair/install workflow with real Gobra and scripted worker subprocesses;
  live model quality is not part of that deterministic qualification.


## v0.2.1

- Prevent `status` from accepting historical success while another project
  operation is active; honor status cancellation and recheck authorization after
  input/log inspection.
- Reject unsigned approval/import operations that would replace a signed review,
  preserving the active record on failure. Authorized signed renewal still works.
- Reassess saved Gobra and named Go-test output before accepting successful
  receipts. Require the exact targeted postcondition failure for every killed
  mutant; reject missing/truncated logs and contradictory verifier summaries.
- Clean up descendants when a check parent exits, including detached children.
  Windows uses a kill-on-close Job Object and registers a suspended root before
  allowing user code to run; unsupported containment fails closed.
- Require the final successful package event as well as each named test pass;
  incomplete Go JSON cannot establish success.
- Pin CI action dependencies to immutable commit SHAs and verify feature branches
  through pull requests, avoiding duplicate push/PR proof runs.
- Preserve separate diagnostic stderr while reassessing Go JSON evidence.
- Report the installed module version for `go install ...@v0.2.1` and provide
  persistent, checksummed release binaries with reproducible build metadata.
- Update the independent signed CI gate to the previously qualified v0.2.0 tooling
  commit. New orchestration is tested separately before promotion.

Supported proof language and host/trust boundaries remain explicit. Receipts are
local unsigned records; these checks do not turn them into proof certificates or
establish long-term deployment history.
