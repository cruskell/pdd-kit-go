# ProofSteer

**Proof-Driven Development for Go.**

ProofSteer checks decision rules in production Go code, runs tests for the surrounding behavior, and writes a receipt you can bring to code review.

Start with one decision in an existing Go project. The core workflow works without an LLM account.

[View live report examples](https://proofsteer-demo.cruskell.chatgpt.site) · [Try the login example](#try-it) · [Use it in your project](#use-it-in-your-project) · [Download pdd](https://github.com/cruskell/proofsteer-go/releases/tag/v0.4.0)

## Why proof-driven development?

AI agents can write code quickly. Human review can become the bottleneck when someone has to trace each branch of an authorization or payment rule and check it against the intended behavior. Passing tests confirm the cases they exercise.

Start by describing the rule in plain language. An agent can draft a Go contract, and a human reviewer approves the contract and its assumptions. After implementation, `pdd` runs Gobra to verify that the Go function satisfies the contract for every input it covers.

Consider a withdrawal rule with a balance of 100. Two authentication states, two lock states, and four withdrawal amounts (0, 1, 100, and 101) give **16 cases**.

| Review task | Manual code review | Proof-driven review |
| --- | --- | --- |
| Is this the intended rule, and are its assumptions sound? | Reviewer checks the specification | Reviewer checks the specification |
| Does the Go function make the right decision for these 16 inputs? | Reviewer traces the code | Gobra checks the contract |
| Do HTTP handling and the browser flow behave as expected? | Reviewer checks test coverage and results | Reviewer checks test coverage and results |

## What a proof catches

Suppose the requirement is **allow a login exactly when the account exists, the password matches, and the account is unlocked**. The specification lives beside the production code in a Go comment that Gobra reads.

```go
// @ ensures allowed == (accountExists && passwordMatches && !locked)
func DecideLogin(accountExists, passwordMatches, locked bool) (allowed bool) {
    return accountExists && passwordMatches && !locked
}
```

An implementation that always returns `false` satisfies a rejection-only rule. It violates this contract because valid logins must succeed. The demo asks Gobra to check the real function and then reject three deliberate changes in scratch copies: ignore a bad password, ignore a locked account, and deny everyone. It also runs a named Go test for the boundary cases. The proof starts with the three booleans; the accuracy of account lookup and password checking remains a caller assumption. [Production function](examples/login/login.go) · [Login plan](examples/login/plan.json)

## Try it

Use Go 1.20 or newer and Docker on a 64-bit host. Put the Go install directory on your `PATH`, or use the [checksummed binaries](https://github.com/cruskell/proofsteer-go/releases/tag/v0.4.0). To use a local Gobra JAR, see [troubleshooting](docs/troubleshooting.md).

```sh
go install github.com/cruskell/proofsteer-go/cmd/pdd@v0.4.0
pdd demo
```

`pdd demo` writes the bundled login example into a new `pdd-demo` directory,
imports its demonstration approval, and runs the real Gobra and Go checks.
Docker pulls the pinned Gobra image on the first run. The command prints the
HTML report and receipt paths. Choose another empty name with `--out DIR`.

The report shows the same two requirement results that `pdd prove` prints.

```text
login-decision: proved (formal)
login-boundary: evidenced (empirical)
```

Open the report path to see the two results, three rejected mutations, and links
to the saved logs. Explore the source and review with `pdd --cwd pdd-demo plan`.
After edits, rerun `pdd --cwd pdd-demo prove` and regenerate the HTML with
`pdd --cwd pdd-demo report --out .pdd/results/demo.html`.

## Use it in your project

Pick a production function that makes an important decision. For a runnable walkthrough in a separate Go module, see [prove one function](docs/first-project.md). Keep the rule in the Go core and use named Go tests where the application connects it to HTTP, storage, or external libraries.

1. Run `pdd init` in your repository. Add `// @ ensures` contracts to the production `.go` files and fill in `.pdd/plan.json` with the rule, checks, trust assumptions, and a deliberate negative mutation for each formal clause. [Plan format](docs/plan-format.md)
2. Run `pdd lint` and `pdd plan`. Use `pdd plan --diff` when revisiting an approved contract. Record which guarantees and assumptions a reviewer accepts with `pdd approve --reviewer "$REVIEWER" --accept-tcb "$TRUST_IDS" --expect-digest "$REVIEWED_DIGEST"`, substituting your reviewer, comma-separated trust IDs, and the digest from the plan they inspected. [Signed reviews](docs/signed-reviews.md)
3. Run `pdd prove` and attach the receipt to the change. In CI, import the saved approval before running the same checks. Use named plans to adopt the workflow one component at a time. [CI adoption](docs/ci.md) · [Named plans](docs/named-plans.md)

If you already work with a coding agent, `pdd draft` can turn a request into an editable plan and decision cards using Codex or an external JSON worker. Two separate advisory reviews help check the intent and guarantees before human approval. After approval, `pdd build` can propose repairs in a scratch copy, run the approved checks, and request an advisory gap review. `pdd pr` prepares a PR description from current evidence. [Agent workflow](docs/agent-workflow.md)

## See it in a real request path

| Example | What to look at |
| --- | --- |
| [HTTP wallet](examples/httpwallet/README.md) | A real HTTP handler calls a proved withdrawal rule. Go tests exercise the HTTP boundary and concurrent withdrawals. The optional browser check submits an overdraft and confirms the balance remains available. |
| [Multi-package authorization](examples/authorization/plan.json) | A handler calls local policy methods. PDD checks the dependency package before the caller. |
| [Slice validation](examples/validation/README.md) | A production loop proves a property over every element using permissions, a quantified contract, and loop invariants. |

## What you can rely on

- **Formal proofs cover stated contracts.** Gobra checks supported Go functions and methods, including supported calls across packages in one module. External and standard-library imports in proof packages remain outside the supported scope. [Supported proof inputs](docs/plan-format.md)
- **Go and browser tests provide evidence for the cases they run.** The receipt labels them separately from proofs. Each planned negative mutation must fail on the intended postcondition. A missing test, tool failure, or inconclusive mutation fails verification. [Design](docs/design.md)
- **Receipts tie the result to the reviewed change.** Machine-readable JSON and Markdown include the plan and source digests, commands, outcomes, trust assumptions, and links or references to saved logs. `status` rejects stale evidence. Use Ed25519 approvals with a trusted policy when CI needs to verify reviewer authority. [JSON protocol](docs/cli-json.md) · [Signed reviews](docs/signed-reviews.md)

ProofSteer is an independent implementation of ideas from the Proof-Driven Development Kit research prototype. Its Go verifier is Gobra. [Original PDD Kit](https://githubnext.com/projects/pdd-kit/) · [Feature comparison and limits](docs/reference-comparison.md)

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
PDD_INTEGRATION=1 go test ./internal/pdd -run 'TestGobra.*Integration' -v -count=1
```

The Check workflow runs race tests across Linux, macOS, and Windows and runs the Gobra integrations on Linux. [Check workflow](.github/workflows/check.yml) · [Build and distribution](docs/distribution.md) · [Troubleshooting](docs/troubleshooting.md) · [MIT license](LICENSE)
