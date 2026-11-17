# Troubleshooting and migration

| Symptom | Action |
| --- | --- |
| Missing `go`, Docker image or Java/jar | Run `pdd doctor`; install the reported tool and pull its exact image digest. Use `prove --go /path/to/go` when Go is not on PATH. |
| Gobra timeout | Read the check log, then use an appropriate `prove --timeout 5m`; a timeout never counts as mutant rejection. |
| Missing/skipped named test | Ensure every listed top-level `TestName` actually runs and passes. Fix skip conditions or the plan; cached output and a successful package with no matching tests do not qualify. |
| Input budget exceeded | Review [input scope](inputs.md), exclude unrelated assets or raise bounded limits, then review/approve the changed policy. Never exclude source to make verification pass. |
| Unsupported proof import/build constraint | Keep a supported Go core in complete local packages and test its stdlib/external adapters empirically. See [scope](../README.md#current-scope). |
| Mutant is inconclusive | Inspect its log. It must remain valid Go and fail on its own target postcondition. Syntax/type errors, unrelated clauses, tool failures and timeouts are not kills. |
| Approval stale | Read `plan --diff`, then inspect the full current plan/contracts. Only a reviewer should accept changed intent or trust; do not auto-approve a freshly calculated digest in CI. |
| Saved CI review cannot import | Check selected plan identity and source declarations/annotations against the exported review. Restore the reviewed input or obtain a new review; import never updates it automatically. |
| Interrupted latest run | Re-run `prove` after correcting the cause. The lock releases on process exit; deleting the lock file is unnecessary and can break mutual exclusion. An older green receipt cannot replace an interrupted run. |
| Concurrent operation rejected | Wait for the active proof/review operation to finish or cancel it. Project locks span all named plans. |
| Status stale immediately after output redirection | Store generated JSON under `.pdd/`, not in the snapshotted source tree. Included output changes invalidate freshness. |
| Missing/altered log, summary or manifest | Regenerate evidence with `prove`. Do not repair hashes by hand. Artifacts are unsigned local attestations. |

Old default plans keep `.pdd/plan.json`, `.pdd/approval.json` and
`.pdd/receipts/`. Named plans are opt-in and bind their own identity; copying a
default approval into another plan does not approve it.

Legacy approvals without saved review text can still validate their original
matching default plan. `plan --diff` reports unavailable historical content.
Review/approve again to get a portable export for CI. Migrating input fingerprints
to `pdd-inputs-v2` requires a fresh `prove`, because the new manifest includes file
modes and directories as well as bytes. Matching contract approvals can remain;
any new input policy, annotation/declaration or trust change needs renewed review.

Linux amd64 is the platform qualified with extracted release archives and the
real Docker verifier. Native CI also runs CLI, lock and process tests on macOS
and Windows; their Docker/local-jar setup must still be checked locally.
The pinned Gobra adapter assumes a 64-bit host and does not support 32-bit targets.
