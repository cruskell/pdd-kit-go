# Design and source references

This project independently implements a small Proof-Driven Development loop for Go. The reference PDD Kit is a research prototype using Rust/Verus; no official Go integration or PDD Kit source release was found in the reference project's linked materials on 2026-11-17. This repository does not reuse or claim compatibility with unpublished code.

Primary references:

- [GitHub Next: PDD Kit](https://githubnext.com/projects/pdd-kit/) describes reviewed intent, explicit trusted boundaries, formal and empirical verification, mutation checks and receipts.
- [Microsoft Research: Formally verifying agent-written full-stack web apps with PDD Kit](https://www.microsoft.com/en-us/research/blog/formally-verifying-agent-written-full-stack-web-apps-with-pdd-kit/) describes the proof-driven workflow and its research status.
- [Gobra source and installation](https://github.com/viperproject/gobra) supplies the Go verification backend.
- [Gobra tutorial](https://github.com/viperproject/gobra/blob/master/docs/tutorial.md) documents preconditions, postconditions and partial correctness.
- [Gobra's Go annotation preprocessing](https://github.com/viperproject/gobra/blob/master/src/main/scala/viper/gobra/frontend/Gobrafier.scala) supports verification annotations embedded in ordinary Go comments.

The selected image is Gobra `v26.09`, resolved and pinned to `sha256:5e96431b06c2e6209aa767df9153bb4cf8b2620598598f1d7d3f3a74c09c14f9`. It bundles Gobra, Viper and Z3. Its console banner reports `0.1.0-SNAPSHOT`; the image digest, not the banner, is its reproducible identity here. The Docker adapter enables overflow checks and uses Gobra's default 64-bit integers.

## Components

- `cmd/pdd` is the executable entry point.
- `internal/cli` parses typed commands and exposes review/approval operations.
- `internal/pdd/plan.go` validates the intent-to-contract map, freezes contracts and records approval assertions.
- `packages.go` resolves complete same-module proof packages, receiver-qualified symbols and the dependency DAG. It rejects ambiguous import resolution and unverified callee packages.
- `inputs.go` validates reviewed asset exclusions and the protected Go input closure; `snapshot.go` captures bounded inputs, modes and directories and creates separate check/mutation workspaces.
- `approval_transfer.go` exports/imports complete matching reviews without minting CI approvals.
- `runner.go` invokes only Gobra and named Go tests, distinguishes operational failures and validates actual results.
- `prove.go` joins the evidence, rejects gaps and staleness, and writes receipts.

Formal evidence covers exactly the clauses checked by the Gobra invocation. The production function is both compiled by Go and passed directly to Gobra. The baseline check succeeds first; each mutant must then fail on the target clause. No successful test can replace a failed formal obligation.

The CLI is an orchestration tool, not another theorem prover. Its approval records, parser, command runner, snapshots and receipt generation are tested empirically. The login, multi-package authorization and HTTP wallet decision contracts are formally checked by Gobra. Test doubles exercise orchestration failure paths; a separate integration test uses the real verifier. No test-double outcome is presented as a real formal verification result in the delivered demonstration.

## Trust and limitations

The plan and annotated files are reviewed inputs. Gobra's language semantics and compiler pipeline, Viper, Z3, the Go compiler/runtime, test assertions and host process execution are part of the trusted environment. External app behavior is represented by accepted boundary assumptions with explicit empirical checks.

Plan validation is strict but is not a proof of intention, completeness, satisfiability or termination. The adapter rejects some known proof bypass constructs, but is not a complete security audit of the verification language. Local imports require complete checks for every transitive callee package, with no shadowing `.gobra` headers, replacements, nested modules or external-library stubs. Dependencies are checked in topological order, and both baseline failures and incomplete mutation assurance propagate to caller verdicts. Pure function bodies are approval-bound because they can define the meaning of contracts. External-library trust adapters remain outside the current implementation.

Go tests and local Java invocations have host permissions. Docker restricts its verifier's mount/network; this is not a full adversarial sandbox for the entire tool. Source hashes do not cover environment variables, network services, dependency caches, or excluded output folders. Receipts record tool identities and commits where available but are not signed or remotely authenticated. Do not treat them as tamper-proof attestations.

The first implementation deliberately leaves LLM generation, independent agent review, Chopin integration and PR automation outside the executable. Developers or agents can follow the accompanying workflow and use machine checks as the gate.
