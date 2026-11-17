# Production qualification beyond v0.1

The next stage addresses authority, native runtime behavior and repeatable
operational failure/recovery. A feature being implemented is not evidence of
months of real deployment. This document keeps those claims separate.

| Area | Implemented evidence | Boundary |
| --- | --- | --- |
| Review authority | Ed25519 signatures, independently provisioned reviewer/plan allowlist, expiration, revocation and policy-bound receipts | Key enrollment and trusted host/CI configuration are operator responsibilities |
| Native platforms | Linux/macOS/Windows CI matrix running race tests, real CLI workflows, process-tree cancellation, timeout and crash-lock recovery | Check the actual workflow outcome; formal Docker runs remain on Linux |
| Broader verified code | Slice loops, quantified postconditions, read permissions and invariants, with real Gobra and mutants | Gobra-supported Go; arbitrary stdlib/external implementations and generic receivers remain outside formal scope |
| Operational qualification | Repeat signed gates, reject a wrong implementation, preserve latest negative evidence and recover after repair | Finite repository qualification, not real deployment history |
| Application boundary | Real HTTP/TCP/concurrency tests connected to proved production decisions | External identity/storage/runtime assumptions remain explicit |

The `authenticated-gate` job builds orchestration from a pinned, separately checked
commit. Its public authority policy comes from a repository variable outside the
incoming source tree. CI imports a stored signature; no private key is available
and no approval is minted. The committed operational-test identity is intentionally
distinct from a human maintainer. A production team must enroll its own reviewer
keys and apply branch/workflow protection.

Run repeat qualification in a disposable clone with a trusted installed binary
and public policy (no signing key is needed):

```sh
python3 scripts/qualify.py --pdd /trusted/bin/pdd --cwd /tmp/disposable-repo \
  --plan validation --policy /trusted/policy.json \
  --approval .pdd/reviews/validation-signed.json --iterations 3 --validation-recovery
```

Initialize the named plan from `examples/validation/plan.json` first. The optional
recovery probe deliberately edits that example's implementation and restores it;
use a disposable clone, not an active checkout. The report under
`.pdd/results/qualification.json` states `production_deployment_history: false`.
It records each source digest, policy identity, receipt and timing, then checks a
real proof failure and subsequent recovery. Native process tests independently
cover cancellation and crash cleanup.

For an actual deployment pilot, choose the target repository, enumerate its Go
language/dependency constraints and external assumptions, enroll real reviewers,
set required CI checks, and observe changes, failures and incident recovery under
that team's workflow. No finite demo run can establish long-term operating history
or universal support for arbitrary Go applications. Unsupported proof inputs must
continue to fail explicitly rather than being passed off as verified libraries.

Observed native qualification: [Check run 44099596974](https://github.com/cruskell/pdd-kit-go/actions/runs/44099596974)
passed Linux, macOS and Windows race/CLI/process tests, real verifier integrations
and the signed-authority job. The current authenticated gate covers both the
permission-aware slice core and the HTTP wallet with three repeated proofs per
plan. Consult the current commit's run rather than treating this historical run
as certification of subsequent changes.

## v0.2.1 integrity audit

`internal/pdd/integrity_test.go` reproduces and prevents concurrent-status reuse,
unsigned replacement of a signed approval, and successful receipt metadata whose
logs do not support the named tests/proofs or targeted mutation rejection. It also
covers canceled status, contradictory verifier summaries and truncated mutant
output. Diagnostic stderr remains separate from Go JSON events when saved evidence
is reassessed. Signed renewal remains supported. The real Gobra integration suite
and native platform jobs exercise these checks with the shipped examples.

The public repository's main branch requires proof, authenticated-gate and the
three native test jobs, including for administrators; force pushes and deletion
are disabled. This enforces the configured checks, not a claim of long-term
production history or support for every possible Go library.

Process qualification also covers a parent that exits while descendants retain its
pipes, and one whose descendants have detached their output. Unix cleanup runs on
every completion path. On Windows, the root starts suspended, is assigned to a
kill-on-close Job Object, and is resumed only after containment is established.
Cancellation terminates the job; completion closes it, cleaning up descendants.
Unexpected startup layouts or unavailable job assignment fail closed. This is
lifecycle containment, not an OS security sandbox for hostile project code.
The Windows implementation follows Microsoft's [Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)
and [process creation flags](https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags)
documentation. Native CI must pass before this change is promoted.
