# pdd-kit-go

**Proof-Driven Development for Go.** Review intent and contracts, prove production Go with Gobra, test the trusted boundary, and keep a receipt of exactly what ran.

This is an independent, working prototype inspired by [Microsoft Research and GitHub Next's PDD Kit](https://githubnext.com/projects/pdd-kit/). It is not an official port or affiliated project. The reference prototype uses Rust/Verus; this project uses [Gobra](https://github.com/viperproject/gobra) for Go. It has no Product-Driven Development dependency.

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

`init` creates an incomplete, unapproved scaffold; it deliberately fails validation until real requirements and checks are supplied. See the complete [example plan](examples/login/plan.json), [plan format](docs/plan-format.md), and [agent workflow](docs/agent-workflow.md).

| Command | Behavior |
| --- | --- |
| `init` | Create a draft without overwriting a plan |
| `plan` | Display intent, formal clauses, trust assumptions, mutations and exclusions |
| `lint` | Validate mappings against production Go and ensure every formal clause has a mutation |
| `approve` | Record a reviewer assertion bound to the plan, declarations and proof annotations |
| `prove` | Run all approved checks and mutations in scratch copies; write JSON/Markdown receipts and logs |
| `status` | Reject unsuccessful, stale or damaged evidence |
| `doctor` | Check Go and the pinned Docker image, or Java/local jar prerequisites |

Exit codes: `0` success, `1` unmet verification or operational failure, `2` CLI usage error. `prove --timeout 2m` limits each check; `--go /path/to/go` selects Go; `--gobra-jar /path/to/gobra.jar` uses Java instead of Docker.

## What counts as success

- A formal requirement maps to an exact `ensures` clause on a named production function, with at least one negative mutation.
- All Gobra checks exit successfully and emit a verification-run success summary. Overflow checks are enabled; the adapter uses Gobra's 64-bit integer semantics.
- Every named empirical Go test actually executes and passes. Missing, skipped, cached-only or empty test runs are not evidence. Checks use `-count=1`.
- Every formal mutation is valid Go, changes only the target function body, preserves the approved contract, and is rejected on the target postcondition. A parse failure, timeout or missing tool is inconclusive and fails the run.
- Every trust assumption has an empirical check and was explicitly accepted. Tests support assumptions on exercised cases; they do not prove them.
- Source, plan and approval remain current. A changed file makes an old receipt stale. A changed plan, function signature or proof annotation requires review again.

There is no automatic fallback from failed formal verification to a green test result. Receipts label formal proof and empirical evidence separately.

## Receipts and trust

Generated artifacts live under `.pdd/receipts/<UTC-run-id>/`: `receipt.json`, `receipt.md` and per-check logs. Receipts include source/plan SHA-256 digests, reviewer assertion, commit (when present), tool identities, commands, durations, outcomes, mutation results, trust assumptions, exclusions and gaps. They are ignored by Git; CI uploads them as artifacts.

Approvals and receipts are **local, unsigned attestations**, not authentication, a cryptographic proof certificate, or protection against a malicious project owner. Hash checks catch stale or accidentally altered artifacts. The Go compiler, Gobra/Viper/Z3, host environment and dependency behavior remain trusted. Go tests and a local jar execute with your user permissions; scratch copies protect source organization but are not an OS security sandbox. The Docker verifier has a read-only source mount and no network.

## Current scope

The end-to-end workflow is implemented. Natural-language intent is reviewed and mapped by a developer or coding agent; the CLI does not contain an LLM or infer contracts automatically. It does not integrate with Chopin, open pull requests, or implement the reference prototype's multiplayer UI.

The first Gobra adapter accepts **import-free production `.go` files**, named top-level functions, single-line `// @` annotations and explicit postconditions. It rejects `trusted`, `assume`, `inhale`, axioms, bodyless functions, block-style proof comments and method requirement mappings. Imported libraries, methods and more complex Go integrations need a future boundary-aware adapter; Gobra itself supports a much wider language subset.

A proof establishes stated properties under the verifier's semantics and assumptions. It does not show that the intent is complete, a precondition is satisfiable, or every behavior terminates. Negative mutations detect specific weak contracts; they are not a complete vacuity or specification-completeness analysis. The proof applies to the files given to Gobra, not automatically to every caller or the whole application.

Snapshots include regular tracked and untracked files up to 16 MiB each, excluding `.git/`, `.pdd/`, `bin/` and `dist/`. Symlinks are rejected. Keep production code out of excluded artifact directories. Named tests must target one local package.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
PDD_INTEGRATION=1 go test ./internal/pdd -run TestGobraIntegration -v
```

The integration test invokes the real pinned Gobra image, compiles/runs Go boundary tests and checks rejection of a deliberately wrong production function. GitHub Actions runs unit/race tests and that integration test, then generates and uploads the demo receipt. CI approval is explicitly named `ci-demo`; it is a reproducibility exercise, not a human approval of a production specification.

[Design and source references](docs/design.md) · [MIT license](LICENSE)
