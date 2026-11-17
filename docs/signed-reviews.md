# Authenticated review gates

Unsigned local assertions remain available for drafting. A production CI gate can
require an Ed25519-signed, unexpired review issued by an authorized key. Authority
comes from an independently provisioned public policy, not a key embedded in an
incoming change. The signature covers the frozen review, plan identity, reviewer,
accepted assumptions and approval time. It does not sign a verification receipt.

Generate a key outside the project, on the reviewer's trusted workstation:

```sh
pdd keygen --private-key /secure/pdd/reviewer.pem --public-key /secure/pdd/reviewer.pub
```

The private file is PKCS8 PEM, created exclusively with mode 0600 on Unix; existing
keys are never overwritten. Public output contains only an identity fingerprint
and PUBLIC KEY PEM. Keep private keys out of repositories and CI. On Windows,
protect the private file using an appropriate user ACL; the CLI does not claim to
validate Windows ACLs. Use your organization's identity process to enroll keys.
A signature proves possession of an authorized key, not the physical identity or
judgment of the person using it.

An administrator supplies this JSON through protected CI configuration or a
trusted base revision, independently of target changes:

```json
{
  "schema": 1,
  "max_age_hours": 2160,
  "reviewers": [{
    "name": "maintainer",
    "public_key": "-----BEGIN PUBLIC KEY-----\nBASE64_KEY\n-----END PUBLIC KEY-----\n",
    "plans": ["default", "payments"]
  }]
}
```

List allowed plans explicitly. Duplicate reviewer names/keys/plans and invalid
keys are rejected. Maximum age is required (1..8760 hours). Removing a key or its
plan authorization revokes it; replacing the policy makes old receipts stale and
requires a new proof under the new policy. Future timestamps beyond five minutes
and expired reviews fail. Normal clock correctness is part of the trusted host.

After inspecting the complete review and its digest:

```sh
pdd --plan payments plan --json > .pdd/results/review.json
pdd --plan payments --policy /secure/pdd/policy.json approve \
  --reviewer maintainer --expect-digest REVIEWED_DIGEST \
  --accept-tcb YOUR_TRUST_IDS --signing-key /secure/pdd/reviewer.pem
pdd --plan payments --policy /secure/pdd/policy.json export-approval \
  --to .pdd/reviews/payments-v1.json
```

CI needs the exported signed record, the independent **public** policy and a
trusted CLI binary, but no signing key:

```sh
export PDD_REVIEW_POLICY=/runner/trusted/policy.json
pdd --plan payments import-approval --from .pdd/reviews/payments-v1.json
pdd --plan payments prove --json > .pdd/results/prove.json
pdd --plan payments status --json > .pdd/results/status.json
```

`--policy PATH` takes precedence over the environment variable. CI configuration
must require it; do not let a target change select its own public authority.
Signed approvals cannot be proved without a policy. Strict receipts require the
policy again on `status`, which rechecks key authorization and expiration.
Policy changes/expiry during verification invalidate the run. Receipts record
policy digest and signing key fingerprint. Changed implementation bodies can keep
the same approval, but must pass verification again; contract changes need review.

The repository's `authenticated-gate` job uses a separately pinned CLI and a public
policy in the repository variable `PDD_REVIEW_POLICY`. It consumes the committed
`codex-operational-test` signature and runs slice and HTTP-application verification repeatedly.
This is an explicit agent-owned qualification identity with a 90-day review, not
a claim of human production approval. Its private key stays on the local review
workstation, never in the repository or runner. Renew by reviewing again, exporting
a new record and updating the workflow's path; revoke via the external policy.

Protect the CLI, gate workflow and authority configuration under your existing
repository/organization policy. Target tests execute host code: run them in an
isolated, disposable worker without secrets, with read-only trusted tooling where
possible. This mechanism is not an adversarial sandbox or a signed proof system.
