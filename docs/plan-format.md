# Plan format (schema 1)

`.pdd/plan.json` is the reviewed intent artifact. Unknown JSON fields and repeated keys (including equivalent case variants) are rejected. JSON artifacts are limited to 8 MiB. The CLI reads only this file; changing the plan never silently changes the implementation.

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

`.pdd/approval.json` binds the normalized plan plus proof annotations and Go declarations to a self-asserted reviewer, accepted trust IDs and timestamp. Implementation bodies can change without changing the contract approval, but mutations still have to match. Changing proof annotations, function signatures, constants, types, the imported module identity or intent requires renewed review. Pure helper bodies define terms used in specifications, so their bodies are also bound to approval; a helper implementation cannot silently weaken the meaning of an unchanged `ensures` clause. This includes invariants: the prototype freezes every `// @` annotation, not just postconditions.

`prove` takes a digest of the repository files, executes isolated check copies, compares the current source and plan afterward, and emits a receipt. Each baseline check receives a fresh snapshot and must not modify it. Receipts record formal dependency check IDs, and `status` checks freshness, log hashes and dependency coverage. New source edits invalidate old receipts even when the prior checks passed. Re-run verification after editing.

State transitions are conservative:

```text
draft -> valid -> approved -> proved/evidenced + mutations killed -> current receipt
                       |                      |
                  contract edit          source edit
                       v                      v
                 pending approval        stale receipt
```
