# CLI JSON protocol (envelope schema 1)

Pass `--json` before or after the command. Stdout contains exactly one JSON
object, including ordinary command failures and usage errors. Progress and
human diagnostics go to stderr. An unwritable stdout fails the command; no CLI
can promise a JSON payload when its output channel is broken.

```json
{
  "schema": 1,
  "command": "lint",
  "plan": "authorization",
  "ok": true,
  "exit_code": 0,
  "data": {"valid": true, "digest": "...", "plan_path": ".pdd/plans/authorization.json", "requirements": 4, "checks": 3}
}
```

The [envelope JSON Schema](cli-output.schema.json) defines the stable outer
shape. Consumers must check **both the process exit code and `ok`**. The `command`
says what succeeded: a successful `plan`, `lint` or `init` is not a successful
verification. New optional fields may be added to command data; do not reject
unknown fields inside `data`.

| Command | `data` on success or an inspectable result |
| --- | --- |
| `init` | `path`, `draft: true`, optional imported `source` and `validation_error`; no approval is created |
| `plans` | Array of `{id, name?, file, valid, error?}`; valid means structurally valid, not proved |
| `plan` | `digest`, `bundle`, `verification_order`, `dependencies`, `approval`, `changes_available`, `changes` |
| `lint` | `valid`, `digest`, `plan_path`, requirement/check counts |
| `export-approval`, `import-approval` | Existing complete approval assertion; reviewer and timestamp are preserved |
| `keygen` | Public identity only: `algorithm`, `key_id`, `public_key`; never private key material |
| `approve` | Approval assertion with plan identity, reviewer, digest, trust IDs, timestamp and frozen `review` |
| `prove`, `status` | `current`, optional `receipt`, optional JSON receipt `path` |
| `doctor` | Ordered prerequisite `checks` and the pinned-image `docker_pull` command |
| `--version`, `--help` | `version` or `help` text |

`prove` and `status` can return a receipt **and exit nonzero**. A failed but fresh
verification has `current: true` and `receipt.success: false`. A historical receipt
can retain its original `success: true` while `current: false` and the envelope
fails because source, approval or plan changed. Never use a historical receipt's
`success` field as the current verdict. A missing result has no `receipt` field.

Errors use `{kind, message, hint?}`. Kinds include `usage`, `validation`,
`initialization`, `approval`, `review`, `execution`, `verification`, `status`,
`prerequisite`, and `io`. Messages explain the concrete failure; automate on the
kind and exit code rather than parsing message wording. Exit codes are `0`
success, `1` unmet checks or operational failure, and `2` invalid CLI usage.

## Review and compare-and-approve

```sh
mkdir -p .pdd/results
pdd --plan authorization plan --json > .pdd/results/authorization-review.json
# A reviewer inspects bundle, trust assumptions and changes in that file.
# Copy that reviewed file's data.digest here; do not auto-approve a freshly read digest.
pdd --plan authorization approve --reviewer YOUR_NAME \
  --accept-tcb authorization-inputs --expect-digest REVIEWED_SHA256 --json
pdd --plan authorization prove --json > .pdd/results/authorization-result.json
```

`--expect-digest` refuses approval if the current intent, proof annotations,
module identity, declarations or pure definitions differ from the reviewed
snapshot. It does not authenticate the reviewer. Review files and approval
records are unsigned local attestations; repository review policy supplies any
real identity and authorization requirements.

Keep generated result files under `.pdd/` (excluded from source snapshots). Redirecting output into a snapshotted source-tree file changes that tree after verification and correctly makes the next status check stale.

With `--policy` or `PDD_REVIEW_POLICY`, prove/status enforce authorized unexpired signatures. Receipts include `approval_policy` (digest, key ID, reviewer). Signed approvals cannot be proved without a policy. `keygen` and `--signing-key` support the workflow described in [signed reviews](signed-reviews.md).
