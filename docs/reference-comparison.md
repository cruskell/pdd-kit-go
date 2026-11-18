# Comparison with Proof-Driven Development Kit

This is an independent Go implementation, not an official port. The reference
is the [GitHub Next PDD Kit research prototype](https://githubnext.com/projects/pdd-kit/)
and [Microsoft Research's workflow description](https://www.microsoft.com/en-us/research/blog/formally-verifying-agent-written-full-stack-web-apps-with-pdd-kit/).
Those publications describe the behavior; no upstream source/API is imported here.

The reference centers human review on intent and trust, then combines a Rust/Verus
proof with empirical checks and model-assisted gap review. It uses Chopin for
planning. These published workflows are the basis for the comparison below.

| Area | Go implementation after this change | Boundary |
| --- | --- | --- |
| Planning from a request | `draft` reads selected Go sources and returns a structured plan and comment-only contracts | Requires an authenticated external Codex CLI or a JSON worker; model output remains an unapproved draft |
| Ambiguity decisions | Contextual cards, options, consequences, recorded answers and optimistic draft hashes | Recommended options are not automatically accepted |
| Intent artifact | Original request, verbatim excerpts mapped to requirement/decision IDs | Human review determines whether the proposed clauses express the right intent |
| Planning review | Two separate invocations for intent fidelity and guarantee quality; both must be ready before approval when enabled | Advisory opinions, not authenticated independent people or formal proof |
| Trust | Per-clause assumptions, impact if false, named tests, Given/When/Then cases and explicit acceptance of each clause | Tests sample behavior; external library implementations remain outside Gobra's proved core |
| Human-facing views | Offline HTML for requirements, verdicts, negative examples, trust, review and linked artifacts | No Chopin API integration, live multiplayer editor or generated semantic pictures |
| Implementation | Scratch candidate repair loop for Go bodies, tests and opt-in provable proof hints | Frozen declarations, contracts, pure definitions, intent and trust cannot be silently changed |
| Verification | Pinned Gobra proofs, named Go and Playwright tests, per-clause negative mutations and source-bound receipts | Supported Go subset; not Rust/Verus equivalence |
| Gap analysis | Separate advisory reviewer sees source and actual machine results; review binds to the exact receipt | A model cannot turn failed machine verification into success |
| Feedback | Machine/advisory failures lead to bounded repair; changed contracts or unsupported scope stop for a decision | No automatic human signoff |
| Delivery | `pr` previews an evidence-backed body; `pr --create` opens a draft PR for committed, pushed, freshly verified code | Does not push, merge or bypass protection |
| Test pruning | `tasks` suggests formally covered regression scenarios, preserving declared trust/boundary/runtime/integration tests | Suggestions only; no automatic deletion |

The remaining reference-specific gaps are real: Chopin's collaborative planning
surface, Rust async and Verus-specific trusted external function adapters, and
general desktop computer-use orchestration remain open. The built-in Playwright adapter runs reviewed browser suites and records empirical results. Arbitrary Go
generics, dependency imports in formal packages and concurrency proofs retain the
restrictions documented in [the plan format](plan-format.md).

## Validation of this change

The worker protocol, isolated review contexts, decision/approval gates, safe edit
installation, stale gap rejection and repair loop have deterministic subprocess
regressions. `TestGobraWorkflowIntegration` also starts from a wrong production
implementation, repairs it through that protocol, proves it using the real pinned
Gobra, kills its negative mutation, runs its Go boundary test, installs the result
and checks the final receipt/gap binding. The worker in that test is scripted;
model quality and a live hosted-model invocation are not claimed as tested.

See [the executable agent workflow](agent-workflow.md) for commands and worker
integration. Existing plans remain compatible; the new workflow policy is opt-in
for manually authored legacy plans and mandatory in generated drafts.
