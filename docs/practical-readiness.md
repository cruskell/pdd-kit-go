# Practical release acceptance

The target is a usable Proof-Driven Development tool for real Go repositories:
reviewable intent and trust decisions, machine-checked Go core contracts, empirical
checks at real I/O boundaries, reliable receipts, and repeatable local/CI operation.
Formal and empirical guarantees must stay distinct. Supported verifier semantics
and trusted boundaries must be stated rather than silently weakened to pass.

The v0.1.0 scope is a usable, restricted Go verification orchestrator. Acceptance
is assessed on Linux amd64; macOS/Windows binaries are cross-built only. It is
still an independent implementation with explicit supported-language and trust
limits, not a proof of every behavior of arbitrary Go applications.

| Gate | Required evidence | Validation |
| --- | --- | --- |
| Verification integrity | Missing/skipped tests, missing tools, failed contracts, unrelated mutant errors and changed inputs cannot produce success | Regression tests, real Gobra integrations and contradictory-receipt rejection |
| Run lifecycle | Concurrent runs cannot corrupt state; cancellation terminates children; interrupted newest runs never reuse an older green receipt | Linux locks, process-tree/timeout/crash tests and canceled-run receipts |
| Repository inputs | Bounded snapshots and reviewed exclusions cannot omit verified source | Budget, closure, fixture/mode preservation and manifest-integrity tests |
| Everyday workflow | Named plans, reviewable approval changes, JSON output and failure reports | Independent namespace, saved-review and real CLI lifecycle tests |
| Go application integration | Runnable HTTP application with proved production logic and empirical I/O checks | HTTP wallet policy proof, three mutants, boundary tests and concurrent TCP/race tests |
| CI adoption | Reviewed approval is preserved; CI never auto-approves production intent | Import/export tests, committed example reviews and the Check workflow |
| Distribution | Versioned reproducible builds, checksums and clean-checkout operation | Five-target archives, byte-identical repeated builds and extracted Linux binary smoke test |
| Documentation | Scope, trust, examples, migration and troubleshooting match implementation | Linked documentation, runnable examples and local link audit |
| Release audit | All validation above applies to the release commit | Require green Check and Release artifacts workflows for the selected commit |

Formal and empirical guarantees remain distinct. The example reviews are
agent-authored demonstration assertions, not human production approvals. A
production repository must provide its own review/identity policy. Test doubles
exercise orchestration; real Gobra runs cover the shipped contracts and mutations.

## Lifecycle evidence

`internal/pdd/lifecycle_test.go` exercises concurrent proof/approval rejection,
newest-run interruption and recovery, legacy unfinished receipts, canceled-run
receipts, ambiguous JSON, and diagnostic binding to the exact mutated clause.
`internal/pdd/process_linux_test.go` uses real subprocesses to check process-tree
cancellation, timeouts, and kernel-lock recovery after SIGKILL. Windows and macOS
binaries are cross-built; their runtime behavior is not claimed as tested.

Lifecycle tests cover orchestration reliability. The other gates have separate
evidence below; lifecycle success alone does not establish application correctness.

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

## Review and release evidence

`internal/pdd/approval_transfer_test.go` rejects stale intent, incomplete or altered
reviews and concurrent imports, while preserving reviewer, trust and timestamp.
The CLI smoke restores those records from a clean clone, verifies login and HTTP
plans using real tools, checks all mutants and confirms later edits are stale.

`scripts/release.py` pins Go 1.27.4 and builds v0.1.0 archives for five targets with
normalized metadata. Two local builds produced identical files and all checksums
passed. The extracted Linux binary passed `scripts/smoke.py`; the standalone wallet
server also returned the expected successful and denied withdrawals and stopped
cleanly on SIGINT. Runtime validation of other operating systems remains outside
this release's evidence.

The [Check workflow](../.github/workflows/check.yml) runs regression/race/vet/build,
real Gobra integrations and preserved-review CLI verification, uploading receipts.
[Release artifacts](../.github/workflows/release.yml) repeats deterministic builds
and the clean-checkout installed-binary smoke test. Release acceptance requires
both workflows to succeed for the selected source commit; see its GitHub Actions
runs for observed results. Packaging creates downloadable CI artifacts and does
not automatically publish or grant production approval.
