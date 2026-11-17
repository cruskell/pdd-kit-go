# Practical release acceptance

The target is a usable Proof-Driven Development tool for real Go repositories:
reviewable intent and trust decisions, machine-checked Go core contracts, empirical
checks at real I/O boundaries, reliable receipts, and repeatable local/CI operation.
Formal and empirical guarantees must stay distinct. Supported verifier semantics
and trusted boundaries must be stated rather than silently weakened to pass.

This is the release acceptance checklist, not a completion claim. Each gate needs
current executable evidence and documentation before it is checked off.

| Gate | Required evidence | State |
| --- | --- | --- |
| Verification integrity | Missing/skipped tests, missing tools, failed contracts, unrelated mutant errors and changed inputs cannot produce success | In progress |
| Run lifecycle | Concurrent runs cannot corrupt state; cancellation terminates child processes; interrupted newest runs never reuse an older green receipt | Verified locally on Linux; release/CI audit pending |
| Repository inputs | Bounded snapshots work with ordinary repository assets and configurable exclusions without omitting verified source | Implemented; bounded capture, protected closure and manifest integrity tests pass; final audit pending |
| Everyday workflow | Multiple named plans, reviewable approval changes, stable machine-readable output and actionable failure reports | Implemented; local workflow/verifier tests pass, release/CI audit pending |
| Go application integration | A runnable HTTP application with formally checked production logic and empirical tests of real handlers/trusted boundaries | HTTP wallet added; real Gobra proof/mutants and race/concurrent TCP tests pass; final audit pending |
| CI adoption | Reviewed approval is preserved; CI never auto-approves production intent; full verification and receipts run on changes | Import/export and committed example reviews implemented; remote audit pending |
| Distribution | Versioned reproducible builds, checksums, install/use instructions and clean-checkout smoke tests | Deterministic five-target packaging and extracted-binary smoke workflow implemented; executable audit pending |
| Documentation | Supported Go scope, trust model, examples, migration and troubleshooting are accurate and usable | Documentation added; final consistency audit pending |
| Final audit | All gates above verified against the final commit; regression, race, verifier and CI checks pass | Pending |

The initial login and local-package authorization demonstrations remain useful
regressions. They do not alone prove practical readiness of the whole tool or a
real HTTP application. Test doubles exercise orchestration; real Gobra runs must
cover the shipped annotated Go and mutant rejection.

## Lifecycle evidence

`internal/pdd/lifecycle_test.go` exercises concurrent proof/approval rejection,
newest-run interruption and recovery, legacy unfinished receipts, canceled-run
receipts, ambiguous JSON, and diagnostic binding to the exact mutated clause.
`internal/pdd/process_linux_test.go` uses real subprocesses to check process-tree
cancellation, timeouts, and kernel-lock recovery after SIGKILL. Windows and macOS
binaries are cross-built; their runtime behavior is not claimed as tested.

These reliability checks do not close the remaining repository-input, workflow,
application, CI adoption, distribution or documentation gates above.

## Workflow evidence

`internal/pdd/workspace_test.go` checks independent plan namespaces, rejection of
copied approvals/receipts, expected-digest review, changed-on-disk intent and saved
review integrity and precondition ownership changes. `internal/cli/cli_test.go` drives the real CLI API and Go tests
through success, source staleness and failed verification, checking that each
JSON response agrees with its exit code. The shipped authorization example is
formally checked as a named plan by the real Gobra integration test.

## Input and application evidence

`internal/pdd/inputs_test.go` checks byte/file budgets, cancellation, explicit asset
exclusions, protected local imports, embedding, testdata, vendoring and replacement
inputs, mode/empty-directory preservation, review binding and manifest tampering.
The source fingerprint uses the versioned, saved input manifest.

`examples/httpwallet` is a runnable stdlib HTTP application. Its handler calls the
Gobra-verified withdrawal predicate under a balance mutex. Empirical tests cover
request/session boundaries, rejected-request state preservation and concurrent
TCP requests. `TestGobraHTTPIntegration` proves the shipped policy with the pinned
verifier, kills three policy mutants and checks receipt freshness. Static demo
authentication and non-durable balances remain explicit trusted/non-goal boundaries.
