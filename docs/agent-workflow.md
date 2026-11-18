# Executable proof-driven workflow

`pdd draft`, `review` and `build` can use an authenticated Codex CLI or an explicit
JSON worker. Deterministic `lint`, `approve`, `prove` and `status` still work without
a model provider. See the [reference comparison](reference-comparison.md) for scope.

## Plan and settle intent

Start with existing production Go declarations. The planner can add contract
comments, but cannot change implementation code. Save the original request in a
text file, then run:

```sh
pdd --plan checkout draft --request-file /path/to/request.txt --files internal/policy.go --apply
pdd --plan checkout decisions --json
pdd --plan checkout report --out .pdd/checkout-review.html
```

Omit `--apply` to preview a proposal. Replacing an existing draft requires
`--expect-draft HASH`, copied from the inspected `draft_digest`. Generated drafts
record the original request, leave all decision selections empty and require both
planning reviews plus a final gap review. Consequential options include context
and effects; a recommendation never counts as an answer.

```sh
pdd --plan checkout decide --id decision-id --option option-id \
  --reason 'The acceptance direction is required' --expect-draft INSPECTED_DRAFT_HASH
pdd --plan checkout lint
pdd --plan checkout review --role both
pdd --plan checkout plan --json
```

The two reviews use separate ephemeral invocations and do not receive each other's
opinions. One examines intent coverage; the other checks vacuity, assumptions and
overclaimed guarantees. They can report `changes-required`, `needs-decision` or
`unsupported`. Their records are embedded in the plan and bound by human approval.
They are advisory model opinions, not proofs or authenticated reviewer identities.

The human reviews the complete plan, formal clauses, original-word mappings,
trust and opinions, then records approval:

```sh
pdd --plan checkout approve --reviewer YOUR_NAME \
  --accept-tcb boundary-id,assumed-clause-id --expect-digest INSPECTED_PLAN_DIGEST
```

Accept every trust ID and every assumed clause ID. Each clause states its impact
if false, maps to a named empirical test and has a matching Given/When/Then
scenario. Use `--signing-key` and an independently provisioned `--policy` for an
[authenticated authority gate](signed-reviews.md). An unsigned assertion does not
authenticate a human. Never have the implementation agent impersonate the reviewer.

## Build and check gaps

```sh
pdd --plan checkout build --max-iterations 3 --apply
pdd --plan checkout tasks --json
pdd --plan checkout status --json
pdd --plan checkout report --out .pdd/checkout-review.html
```

The loop clones the bounded input snapshot, runs approved machine checks, asks an
implementation worker for structured Go edits when needed, and retries. A separate
gap reviewer compares the source and actual evidence after successful machine
verification. Contracts, declarations, pure definitions, intent and trust stay
frozen. If those must change, the loop stops with `needs-decision`; revise the plan,
repeat reviews and obtain a new human approval. Missing tools and exhausted repair
limits fail closed. A model's `ready` never overrides a machine failure.

With `workflow.allow_proof_hints`, body `assert`, `invariant`, `decreases`, `fold`
and `unfold` hints may change because the verifier must establish them. Public
contracts, pure definitions and all other annotations remain frozen. Existing
plans without this option continue to freeze every annotation. Prefer mutation
`kind: "last-return"` to keep an approved negative case independent of body text;
its replacement must still fail on the intended clause, not just any error.

Without `--apply`, `build` returns a verified candidate and saved edits without
changing production files. With `--apply`, installation checks that source, plan
and approval have not changed, then runs production verification again. Per-iteration
receipts, responses, final edits and `build.json` remain under the selected plan's
`builds/` state directory. The final gap record binds to the exact final receipt.
A later `prove` creates a new receipt; use `review --role gap` again before PR delivery.

`tasks` lists dependencies, formal versus empirical coverage and remaining gaps.
Its pruning candidates are suggestions for declared `formal-regression` scenarios,
never automatic deletions. Boundary, runtime, integration and trust tests remain.

## Deliver a PR

```sh
pdd --plan checkout pr
# Commit and push the feature branch, then verify that committed revision.
pdd --plan checkout prove
pdd --plan checkout review --role gap
pdd --plan checkout pr --title 'Enforce checkout policy' --create
```

The default command only saves/previews `PR.md`. `--create` uses `gh pr create
--draft`, requires a clean feature branch, an exact HEAD-bound current receipt and
matching remote branch. It does not push or merge. Model review is included as an
opinion alongside formal/empirical results, trust and limits. CI restores an
existing reviewed approval using `import-approval`; it never auto-approves intent.

## Worker protocol

All three model commands accept `--driver codex` (default), `--worker /absolute/path`
for another provider, `--files a.go,b.go` and `--agent-timeout 5m`. A worker is a
trusted executable, launched directly without a shell. It receives one schema-1
JSON request on stdin and must emit exactly one strict JSON response on stdout;
diagnostics belong on stderr. It runs in a scratch copy. Source files in the
request are bounded to 512 KiB; choose a narrower `--files` set for large projects.

The request includes a random invocation ID, `request_digest`, role/instructions,
plan/format guide, source/spec digests, selected Go sources and (when available)
the actual machine receipt plus bounded log excerpts (512 KiB total). Planning critics do not receive prior review records.
The response has these fields, all present for the Codex structured-output driver:

```json
{
  "schema": 1,
  "request_digest": "COPY_THE_REQUEST_DIGEST",
  "status": "ready",
  "summary": "Explanation of this result",
  "findings": [],
  "plan_json": "",
  "edits": []
}
```

Findings use `severity` (`blocker`, `warning`, `info`), `kind`, `text` and requirement
IDs in `requirements`. `ready` contradicting a blocker is rejected for reviews.
Planning returns a JSON-encoded plan in `plan_json`; implementation returns complete
Go files in `edits: [{"file":"core.go","content":"..."}]`. Review roles may return
neither plan replacements nor edits. Missing/misbound/malformed output and worker
modifications to its scratch source fail closed. No response creates approval.

The Codex driver uses `exec --ignore-user-config --ephemeral --sandbox read-only`
and structured output. Provider authentication and a compatible installed CLI are
operator prerequisites. This project does not supply a model or attest its quality.
Custom workers and empirical Go tests have host-user privileges; snapshot checking
is integrity checking, not an OS security boundary. Keep signing keys outside the
repository. Use isolated CI hosts for untrusted projects.
