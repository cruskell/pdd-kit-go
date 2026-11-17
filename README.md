# pdd-kit-go

**Proof-Driven Development for Go.** Review intent and contracts, prove production Go with Gobra, test the trusted boundary, and keep a receipt of exactly what ran.

This is an independent, working prototype inspired by [Microsoft Research and GitHub Next's PDD Kit](https://githubnext.com/projects/pdd-kit/). It is not an official port or affiliated project. The reference prototype uses Rust/Verus; this project uses [Gobra](https://github.com/viperproject/gobra) for Go. It has no Product-Driven Development dependency.

Versioned binaries, checksums and reproducible build instructions are in
[distribution](docs/distribution.md). See [CI adoption](docs/ci.md) and
[troubleshooting/migration](docs/troubleshooting.md).

See [authenticated review gates](docs/signed-reviews.md) and
[production qualification](docs/production-readiness.md) for signed authority,
native platform checks and repeatable failure/recovery testing.

Install the tagged CLI with `go install github.com/cruskell/pdd-kit-go/cmd/pdd@v0.2.1`,
or download checksummed binaries from [Releases](https://github.com/cruskell/pdd-kit-go/releases).

## Quick start

The CLI uses only Go's standard library. Build with Go 1.20+ on a 64-bit host. Formal checks additionally require Docker and the pinned Gobra image, or a local Gobra jar with Java and Z3 configured.

```sh
git clone https://github.com/cruskell/pdd-kit-go.git
cd pdd-kit-go
go build -o bin/pdd ./cmd/pdd

docker pull ghcr.io/viperproject/gobra@sha256:5e96431b06c2e6209aa767df9153bb4cf8b2620598598f1d7d3f3a74c09c14f9
bin/pdd doctor
bin/pdd plan
```

The repository's `.pdd/plan.json` is a demo plan for `examples/login/login.go`. Read it, then record **your own** review and acceptance of its input assumptions:

```sh
bin/pdd approve --reviewer YOUR_NAME --accept-tcb identity-inputs
bin/pdd prove
bin/pdd status
```

A successful demo reports:

```text
login-decision: proved (formal)
login-boundary: evidenced (empirical)
```

The `DecideLogin` function is ordinary, compilable Go, with Gobra annotations in comments:

```go
// @ ensures allowed == (accountExists && passwordMatches && !locked)
func DecideLogin(accountExists, passwordMatches, locked bool) (allowed bool) {
    return accountExists && passwordMatches && !locked
}
```

It proves the decision for every boolean input, including accepting valid logins. Three isolated mutations remove the password gate, remove the lock gate, and reject everyone. Each must produce a postcondition failure on the declared clause. Database lookups, password hashing and token issuance are outside this example's proof; its boundary test does not establish those systems' correctness.

## Use in another Go repository

```sh
pdd --cwd /path/to/repo init
# Edit .pdd/plan.json and add // @ contracts to production .go files.
pdd --cwd /path/to/repo lint
pdd --cwd /path/to/repo plan
pdd --cwd /path/to/repo approve --reviewer YOUR_NAME --accept-tcb boundary-id
pdd --cwd /path/to/repo prove
```

`init` creates an incomplete, unapproved scaffold; `init --from PATH` imports an existing plan without running checks or recording approval; it deliberately fails validation until real requirements and checks are supplied. See the complete [login plan](examples/login/plan.json), [multi-package authorization plan](examples/authorization/plan.json), [plan format](docs/plan-format.md), and [agent workflow](docs/agent-workflow.md).

| Command | Behavior |
| --- | --- |
| `init` | Create a draft without overwriting a plan |
| `plans` | List default/named plans, including invalid drafts |
| `plan` | Display intent, formal clauses, trust assumptions, mutations and exclusions; `--diff` shows changes since review |
| `lint` | Validate mappings against production Go and ensure every formal clause has a mutation |
| `keygen` | Create an Ed25519 keypair outside the source tree |
| `approve` | Record a reviewer assertion; `--signing-key` signs the inspected `--expect-digest` |
| `export-approval` | Save an existing complete review with `--to PATH` |
| `import-approval` | Restore a matching review with `--from PATH`, preserving its assertion |
| `prove` | Run all approved checks and mutations in scratch copies; write JSON/Markdown receipts and logs |
| `status` | Reject unsuccessful, stale or damaged evidence |
| `doctor` | Check Go and the pinned Docker image, or Java/local jar prerequisites |

Exit codes: `0` success, `1` unmet verification or operational failure, `2` CLI usage error. `--plan NAME` selects an independent plan and state namespace. `--json` emits a schema-1 result with structured errors. `approve --expect-digest SHA256` binds approval to the snapshot reviewed. See [named plans](docs/named-plans.md) and [JSON protocol](docs/cli-json.md).

`prove --timeout 2m` limits each check; `--go /path/to/go` selects Go; `--gobra-jar /path/to/gobra.jar` uses Java instead of Docker.

## Multi-package example

The [authorization example](examples/authorization/plan.json) verifies a handler that imports a local policy package. `Policy` is declared in `types.go`, `Policy.Allows` implements its value-receiver contract in `policy.go`, and the handler's own contract proves the composed result. A pointer-receiver method, `Policy.IsEnabled`, also proves its read contract while preserving the caller's access permission. The plan intentionally lists the caller first; the CLI runs the callee check first.

Create an independent authorization plan, then review its assumptions:

```sh
bin/pdd --plan authorization init --from examples/authorization/plan.json
bin/pdd --plan authorization lint
bin/pdd --plan authorization plan --diff
bin/pdd --plan authorization approve --reviewer YOUR_NAME --accept-tcb authorization-inputs
bin/pdd --plan authorization prove
bin/pdd --plan authorization status
```

This keeps the default login demonstration and its approval independent. `go test ./examples/authorization/...` exercises the production Go call path without running Gobra.

## Runnable HTTP example

The [HTTP wallet](examples/httpwallet/README.md) puts a verified production decision
behind a real HTTP handler. Tests cover JSON/session boundaries and concurrent TCP
withdrawals; its plan distinguishes empirical evidence from formal proof.

## What counts as success

- A formal requirement maps to an exact `ensures` clause on a named production function or receiver-qualified method, with at least one negative mutation.
- All Gobra checks exit successfully and emit a verification-run success summary. Overflow and injectivity checks are enabled; the adapter uses Gobra's 64-bit integer semantics.
- Every named empirical Go test actually executes and passes. Missing, skipped, cached-only or empty test runs are not evidence. Checks use `-count=1`.
- Every formal mutation is valid Go, changes only the target function body, preserves the approved contract, and is rejected on the target postcondition. The diagnostic must point to the target source file and its exact postcondition line. A parse failure, timeout, unrelated clause failure or missing tool is inconclusive and fails the run.
- Every trust assumption has an empirical check and was explicitly accepted. Tests support assumptions on exercised cases; they do not prove them.
- Source, plan and approval remain current. A changed file makes an old receipt stale. A changed plan, function signature, proof annotation or its owner, imported declaration or pure specification helper requires review again.

There is no automatic fallback from failed formal verification to a green test result. Receipts label formal proof and empirical evidence separately.

## Receipts and trust

Verification and approval are protected by an OS-backed project lock, automatically released when the owning process exits. Concurrent proof, approval and status operations are rejected. `.pdd/run.json` identifies the newest run; an interrupted or incomplete newest run cannot fall back to an older success. SIGINT/SIGTERM cancellation and per-check timeouts terminate child process groups on supported Unix hosts, and the Docker backend removes its verifier container. A canceled run records negative evidence.

Default-plan artifacts live under `.pdd/receipts/<UTC-run-id>/` (named plans use `.pdd/state/NAME/receipts/`): `receipt.json`, `receipt.md`, `inputs.json` and per-check logs. Receipts include source/plan SHA-256 digests, reviewer assertion, commit (when present), tool identities, commands, durations, outcomes, mutation results, trust assumptions, exclusions and gaps. They are ignored by Git; CI uploads them as artifacts.

Default approvals and all receipts are **local attestations**. Optional Ed25519 approvals with `--policy` authenticate authorized signing keys and enforce expiry/revocation; receipts themselves remain unsigned. They are not cryptographic proof certificates or protection against a malicious project owner. Hash checks catch stale or accidentally altered artifacts. The Go compiler, Gobra/Viper/Z3, host environment and dependency behavior remain trusted. Go tests and a local jar execute with your user permissions; scratch copies protect source organization but are not an OS security sandbox. The Docker verifier has a read-only source mount and no network.

## Current scope

The end-to-end workflow is implemented. Natural-language intent is reviewed and mapped by a developer or coding agent; the CLI does not contain an LLM or infer contracts automatically. It does not integrate with Chopin, open pull requests, or implement the reference prototype's multiplayer UI.

The Gobra adapter accepts production `.go` packages, top-level functions and named-receiver methods (for example `Policy.Allows`), single-line `// @` annotations and explicit postconditions. Type declarations can live in separate files. Each proof check lists **all production Go files** in one package.

Imports inside the root Go module are supported when every imported package has its own complete Gobra check. The CLI resolves dependencies, verifies callees before callers, records those dependencies in receipts and propagates incomplete dependency verification to caller verdicts. Importing a contract alone never counts as verifying its implementation.

The adapter rejects external/standard-library proof imports, `go.mod` replacements, nested modules, explicitly build-tagged or host-excluded proof files, dot/side-effect imports and `.gobra` headers that could shadow production Go. It also rejects `trusted`, `assume`, `inhale`, axioms, bodyless functions and block-style proof comments. Keep external dependencies behind explicit input assumptions; external-library adapters and generic receivers remain future work.

A proof establishes stated properties under the verifier's semantics and assumptions. It does not show that the intent is complete, a precondition is satisfiable, or every behavior terminates. Negative mutations detect specific weak contracts; they are not a complete vacuity or specification-completeness analysis. The proof applies to the files given to Gobra, not automatically to every caller or the whole application.

Snapshots include regular tracked and untracked files and empty directories, preserving permissions. Defaults are 16 MiB per file, 128 MiB total and 20,000 files; `.git/`, `.pdd/`, `bin/` and `dist/` are excluded. A reviewed `inputs` policy can raise limits and exclude unrelated assets. Required Go sources, local dependencies, embedded inputs and testdata cannot be excluded. Symlinks and special files are rejected. See [input scope](docs/inputs.md). Named tests must target one local package.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
PDD_INTEGRATION=1 go test ./internal/pdd -run 'TestGobra.*Integration' -v -count=1
```

The integration tests invoke the real pinned Gobra image, compile/run Go boundary and HTTP tests, verify an importing handler, a separate policy package with a value-receiver method and the wallet decision, and reject deliberately wrong production implementations. GitHub Actions runs unit/race tests and that integration test, then generates and uploads the demo receipt. CI imports committed `codex-example-review` assertions; it never auto-approves current intent. These are agent-authored demonstration reviews. See [reviewed CI adoption](docs/ci.md) for production use.

[Practical release checklist](docs/practical-readiness.md) · [Design and source references](docs/design.md) · [MIT license](LICENSE)
