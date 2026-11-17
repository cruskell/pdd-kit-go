# Independent named plans

The default plan remains `.pdd/plan.json`. Existing default approvals and receipts
remain readable. Explicit `--plan default` is identical to omitting `--plan`.
Named plans use the same schema-1 content, with identity supplied by the CLI:

```sh
pdd --plan payments init
# Or import an existing plan as a draft; this neither runs checks nor approves it.
pdd --plan authorization init --from examples/authorization/plan.json
pdd plans
pdd --plan authorization lint
pdd --plan authorization plan --diff
```

Names start with a lowercase letter and contain lowercase letters, digits and
hyphens. Traversal, absolute paths, separators and whitespace are rejected.
`default` is reserved for the compatible default path. An absent named plan is an
error; it never falls back to a default plan. `init` never overwrites an existing
plan, including under concurrent pdd invocations. Imported plans with missing Go
sources or mappings remain drafts; the validation problem is returned for repair
before approval.

| Selected plan | Authored file | Approval and latest-run state | Receipts |
| --- | --- | --- | --- |
| default | `.pdd/plan.json` | `.pdd/approval.json`, `.pdd/run.json` | `.pdd/receipts/` |
| authorization | `.pdd/plans/authorization.json` | `.pdd/state/authorization/approval.json`, `.pdd/state/authorization/run.json` | `.pdd/state/authorization/receipts/` |

Commit authored plan files with the Go contracts. Generated state is ignored.
Every approval, run record and receipt is bound to its plan identity. Even two
plans with identical content have different named digests; copying an approval or
receipt between them does not authorize or prove the other plan. Plans share the
project OS lock so tests cannot race through the same project services; their
stored states are independent. A shared source edit can invalidate both plans'
evidence, which must then be run again.

## Review changes

New approvals retain the normalized intent, frozen annotations/declarations, annotation ownership and
pure definitions that were reviewed. `plan --diff` compares current content with
that saved review, keyed by requirement/check/mutation ID and source path. The
JSON form exposes `changes` with `path`, `before` and `after`. Ordinary implementation
body changes do not become contract changes; proof annotations and pure definition
changes do. All current verification still needs to run against the new source.

A legacy approval can remain current without containing historical review text.
For that record the CLI says `changes_available: false`; it does not invent a
previous spec or report an empty diff as proof that nothing changed. Damaged saved
review content is rejected rather than used as a trusted comparison baseline.

For the final review step, use `approve --expect-digest REVIEWED_SHA256`. The plan
on disk must still be the plan loaded, and the frozen review must still match the
expected digest. A failed comparison leaves the previous approval untouched.
Changing a named plan's approval does not overwrite the default or another plan.

Approval compatibility does not preserve an unsafe interpretation: older records that lack ownership binding for `requires`, `preserves`, pure/ghost annotations or other non-postcondition clauses need renewed review. Moving a precondition from a helper onto the verified function changes its guarantee even if all annotation text stays in the same order. The current digest detects that change. Simple default plans with only mapped function `ensures` clauses retain their previous digest. Historical records remain readable, and missing review text is labelled rather than invented.
