# Plan format (schema 1)

`.pdd/plan.json` is the default intent artifact; named plans live under `.pdd/plans/NAME.json` and are selected with `--plan NAME`. Unknown JSON fields and repeated keys (including equivalent case variants) are rejected. JSON artifacts are limited to 8 MiB. The CLI reads only this file; changing the plan never silently changes the implementation.

The [login example](../examples/login/plan.json) is a complete plan. Identifiers start with a lowercase letter and contain lowercase letters, digits and hyphens. IDs are unique across requirements, checks, trust assumptions and mutations.

| Field | Meaning |
| --- | --- |
| `schema` | Must be `1` |
| `name`, `intent` | Human-readable purpose and intended behavior |
| `non_goals` | Limitations and excluded guarantees shown in receipts |
| `requirements` | Named obligations with a formal or empirical evidence classification |
| `checks` | Typed Gobra or Go-test invocations; arbitrary shell commands are not supported |
| `trusted_boundary` | Explicit assumptions with empirical checks |
| `mutations` | Negative examples, at least one per formal requirement |
| `inputs` | Optional reviewed snapshot limits and exact asset exclusions; see [input scope](inputs.md) |

## Requirements and checks

A formal requirement names `id`, `text`, `kind: "formal"`, `check`, `file`, `function`, and `ensures`. The `ensures` value must exactly match a single-line `// @ ensures ...` comment attached to that function's Go doc comment. Methods use `Type.Method` identities for both value and pointer receivers, such as `Policy.Allows`; unqualified method names are rejected. Generic receivers are not yet supported. Pointer contracts can use Gobra permission annotations such as `// @ preserves acc(p)`; the caller must satisfy those preconditions. Permission/frame annotations are approval-bound, while each explicit `ensures` clause has its own requirement and mutation. Every function postcondition in a proof file must map to a requirement, and a clause cannot be mapped twice. Distinct clauses should have distinct requirements and negative mutations.

A `gobra` check names `id`, `kind: "gobra"`, and `files`, an explicit list of **all production `.go` files** in one directory and package. Files containing only types and constants are allowed. Duplicate files, omitted production files, multiple checks for the same package and explicitly build-tagged or host-excluded files are rejected. `_test.go` files are empirical checks, not formal inputs.

The adapter invokes the pinned Docker image with overflow and injectivity checks. It never proves a hand-maintained `.gobra` copy while shipping different Go code. For local imports, it supplies the root module identity and checkout include path to Gobra. Every imported package in the main module must be represented by its own complete Gobra check; dependency-only declaration packages need no invented product requirement, but any `ensures` clauses still require explicit mappings. Dependencies determine execution order regardless of plan order. A failed dependency baseline blocks caller proof execution, and a dependency's surviving or inconclusive mutation leaves caller requirements unproven.

External/standard-library imports, nested modules, `go.mod` replacement directives, requirements on child modules, dot/side-effect imports and `.gobra` files alongside proof sources are rejected. These restrictions keep Go and Gobra resolving the same import to the same reviewed production source. Empirical test packages can still use ordinary Go dependencies.

An empirical requirement names `id`, `text`, `kind: "empirical"`, and a `go-test` check. That check has `package` (one local package, such as `./internal/auth`) and `tests` (explicit top-level names such as `TestLoginBoundary`). Passing output must contain an actual `pass` event for every named test. Table cases/subtests may exist inside those named tests. Test quality and sufficiency require review; a passing test can still assert nothing useful.

Each trust item has `id`, `assumption`, and `check`; the check must be empirical. `approve --accept-tcb` must list exactly all trust IDs. Mapping a broad assumption to one weak test does not make it true: explain the test's limits in the assumption and non-goals.

## Negative mutations

Each mutation has `id`, `requirement`, `check`, `file`, `find`, and `replace`. The check and file must match the formal requirement. `find` must occur exactly once, wholly inside the named function's body. The replacement must differ, remain parseable Go, and retain the same proof annotations and declarations. Type-invalid mutations may pass plan lint but will be inconclusive when Gobra runs; they cannot count as killed mutants.

Mutations execute on separate snapshots. A mutant is killed only when Gobra returns nonzero with a postcondition diagnostic and the expected clause's `Assertion ... might not hold.` text at the target file and exact authored postcondition line. A failure at an identical expression in another function is not accepted. A different diagnostic format is conservatively inconclusive. This parser is tested against the pinned Gobra image; local jars with other formats may require a future adapter update.

## Approval and freshness

The selected plan's approval record binds its identity, saved review, normalized plan plus proof annotations and Go declarations to a self-asserted reviewer, accepted trust IDs and timestamp. Implementation bodies can change without changing the contract approval, but mutations still have to match. `plan --diff` compares the current frozen content with the saved review; `approve --expect-digest SHA256` rejects changes since that review. Legacy default records remain readable and explicitly report when historical review text is unavailable. Changing proof annotations, their owning declarations, function signatures, constants, types, the imported module identity or intent requires renewed review. A precondition moved from a helper to the verified function is a contract change even when annotation text and declaration order stay the same. Pure helper bodies define terms used in specifications, so their bodies are also bound to approval; a helper implementation cannot silently weaken the meaning of an unchanged `ensures` clause. By default this includes invariants. An explicit `workflow.allow_proof_hints` option permits provable body hints while freezing contracts, pure definitions and other annotations; see [the workflow](agent-workflow.md).

`prove` takes a digest of the repository files, executes isolated check copies, compares the current source and plan afterward, and emits a receipt. Each baseline check receives a fresh snapshot and must not modify it. Receipts record formal dependency check IDs, and `status` checks freshness, log hashes and dependency coverage. New source edits invalidate old receipts even when the prior checks passed. Re-run verification after editing.

State transitions are conservative:

```text
draft -> valid -> approved -> proved/evidenced + mutations killed -> current receipt
                       |                      |
                  contract edit          source edit
                       v                      v
                 pending approval        stale receipt
```

## Optional proof-driven planning fields

`original_request` preserves the request; `intent_map` maps verbatim excerpts to
requirement/decision IDs. `decisions` records contextual options and human answers;
an unresolved card blocks approval. `scenarios` uses Given/When/Then with a named
Go test and purpose (`boundary`, `integration`, `runtime`, `formal-regression`).
A trusted boundary can add `affects` requirement IDs and `clauses` with distinct
`id`, assumption `text`, `impact` if false and selected `test`. Every clause needs a
matching scenario and individual `--accept-tcb` acceptance alongside its boundary.

`workflow` optionally sets `require_dual_review`, `require_gap_review`, and
`allow_proof_hints`. Generated drafts enable all three. `planning_reviews` contains
strict advisory intent/guarantee records included in the approval digest. These
fields are optional for legacy plans. Full CLI usage and worker response formats
are in [the workflow guide](agent-workflow.md).

Mutation `kind: "last-return"` replaces the lexically last return in the mapped
function, skipping nested function literals. Set `find` to an empty string and
`replace` to a return statement. This stays applicable when the implementation
body changes; a surviving/no-op mutant still cannot produce success. Omitted kind
or `kind: "text"` retains the existing exact-once text behavior.
