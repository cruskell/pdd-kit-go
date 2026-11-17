# Reviewed intent in CI

A CI run must consume an existing review. Never run `approve` automatically for
production changes: computing a fresh digest and accepting it in the same job
would discard the review boundary.

After inspecting `plan --diff` and the complete `plan --json` output locally:

```sh
pdd approve --reviewer YOUR_NAME --accept-tcb YOUR_TRUST_IDS \
  --expect-digest REVIEWED_DIGEST
pdd export-approval --to .pdd/reviews/default-v1.json
git add .pdd/plan.json .pdd/reviews/default-v1.json
```

An export contains the frozen plan, annotations, declarations and accepted trust
assumptions. Commit it through your normal repository review. Exports never
silently overwrite a different record; choose a new path for the next review and
update the workflow reference. Active local state remains ignored by Git.

In a clean checkout, with the CLI, Go and pinned Gobra image installed:

```sh
pdd import-approval --from .pdd/reviews/default-v1.json
pdd prove --json > .pdd/prove-result.json
pdd status --json > .pdd/status-result.json
```

Both commands must exit zero. If consuming JSON, require `ok`, `exit_code == 0`,
`data.current` and `data.receipt.success`. Upload `.pdd/receipts/` and the result
files even on failure. Named plans add `--plan NAME` to all commands and upload
`.pdd/state/NAME/receipts/` instead. Import preserves reviewer, trust acceptance,
review digest and timestamp; changed contracts/intent/trust or the wrong plan
fail before tools run. Ordinary implementation changes can retain approval but
still require a fresh proof. Legacy records without frozen review must be
reviewed and approved again before export.

The repository's [workflow](../.github/workflows/check.yml) exercises this pattern
for the login and HTTP wallet examples using committed `codex-example-review`
assertions. These are explicitly agent-authored demonstration reviews, not human
approvals of a production system. Integration test fixtures also create isolated
demo assertions to test the orchestrator. No workflow step mints a new approval.

For production, protect changes to intent, annotations, `.pdd/reviews/**` and CI
configuration with your existing reviewer/branch policy. Require the proof job
before merging and keep its CLI/verifier version pinned. The CLI's unsigned,
self-asserted reviewer field does not authenticate a person or resist a malicious
repository owner. Repository identity and authorization remain external policy.
Run untrusted PR tests only on appropriately isolated workers with read-only
permissions and no production secrets; the scratch copy is not an OS sandbox.
