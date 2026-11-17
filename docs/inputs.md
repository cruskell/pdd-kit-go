# Repository input scope

Every check and mutation starts from a fresh copy of the captured repository.
Tracked and untracked regular files count; `.gitignore` does not define proof scope.
The root `.git`, `.pdd`, `bin` and `dist` directories are always excluded. Keep
verified source out of those directories. Symlinks and special files fail capture.
Empty directories and permission bits are preserved, including executable fixtures.

Defaults bound capture to 16 MiB per file, 128 MiB total and 20,000 files, with a
separate directory/file entry limit of four times the file limit. Large unrelated
assets can be excluded in the selected plan:

```json
"inputs": {
  "exclude": ["recordings", "docs/generated-video.mp4"],
  "max_file_bytes": 33554432,
  "max_total_bytes": 268435456,
  "max_files": 40000
}
```

Paths are exact, canonical, project-relative names. A directory excludes its whole
subtree; globs, traversal, absolute paths and duplicates are rejected. Omitted or
zero limits use defaults. Maximums are 256 MiB per file, 512 MiB total and 100,000
files. Changing the policy changes the review digest and requires approval again.
Exclusions change the evidence boundary: tests must not depend on excluded assets.

Lint statically follows local Go imports from every proof and empirical package.
Go/build sources, root module metadata, package `testdata`, matched `go:embed`
inputs, vendored implementations and in-project module replacements are protected
against exclusions. Out-of-project local replacements cannot be captured and are
rejected for empirical checks. Formal checks retain their stricter module rules.
External module implementations, the Go toolchain and host environment remain
trusted. Empirical commands disable parent Go workspaces and ambient GOFLAGS.
Arbitrary file reads by application code cannot be discovered statically; review
must ensure any additional runtime fixture remains in the snapshot.

`inputs.json` lists each included path, kind, permission mode, size and file SHA-256.
Its canonical content defines the `pdd-inputs-v2` source digest. Receipts record the
effective policy, file/directory counts, byte total and manifest artifact hash.
`status` compares the current snapshot, summary and saved manifest before accepting
the result. Changes to an excluded asset do not invalidate evidence; changes to
included bytes, membership or permissions do. Oversized inputs fail closed, and
cancellation during capture records an interrupted latest run rather than reusing
an older green receipt. JSON artifacts are independently limited to 8 MiB.

Receipts made before the v2 input manifest must be regenerated with `prove`.
Existing contract approvals remain usable when their review digest still matches;
the input algorithm change alone does not approve new intent.
