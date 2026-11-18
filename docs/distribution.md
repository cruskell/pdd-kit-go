# Install and reproduce a release

Versioned binaries and checksums are published on [GitHub Releases](https://github.com/cruskell/pdd-kit-go/releases).
Go users can install the tagged CLI directly:

```sh
go install github.com/cruskell/pdd-kit-go/cmd/pdd@v0.3.0
pdd --version
```

The installed version comes from Go module build information. Release archives
embed the same version explicitly; local checkout builds show a development
version. Verification still needs the Go toolchain and the pinned Gobra backend.

The CLI has no third-party Go dependencies. Development builds need Go 1.20+:

```sh
go build -o bin/pdd ./cmd/pdd
bin/pdd --version
```

Release builds use the exact Go 1.27.4 toolchain. Fetch it from
[official Go downloads](https://go.dev/dl/) and verify its published SHA-256.
From a clean checkout of the release commit, with Python 3 available:

```sh
GO=/path/to/go1.27.4/bin/go python3 scripts/release.py v0.3.0
```

The script produces five archives under `dist/v0.3.0`: Linux amd64/arm64,
macOS amd64/arm64 and Windows amd64, plus `BUILD.json` and `SHA256SUMS`.
It disables CGO, VCS stamping and local file paths, embeds the requested CLI
version, records the commit/toolchain/flags, and normalizes archive metadata.
Archives use uncompressed payloads to avoid zlib-version differences. Output
must be empty and the checkout clean; stale artifacts are never reused.

To check reproducibility, run the same version twice into different empty output
directories and compare `SHA256SUMS` (or all files). The same checkout, toolchain,
flags and target architecture must produce identical bytes. The release workflow
runs this check before uploading the versioned artifacts.

Before installing, verify checksums in the distribution directory:

```sh
sha256sum --check SHA256SUMS
# macOS: shasum -a 256 -c SHA256SUMS
# Extract the archive for your OS/architecture, then copy pdd to a PATH directory.
pdd --version
pdd --help
pdd doctor
```

On Windows, compare `Get-FileHash -Algorithm SHA256 ARCHIVE.zip` with its line
in `SHA256SUMS`, extract the zip, and put `pdd.exe` on PATH. Checksums detect
corruption; obtain them from the same trusted release source as the artifacts.
macOS/Windows archives are cross-built. Native CI now exercises Go/CLI workflows,
locks and process cancellation on Linux, macOS and Windows; extracted release
archives with the real Docker verifier are qualified on Linux amd64. This project does not claim notarization or code signing.

The executable alone does not bundle Go, Docker or Gobra. Empirical checks need
a Go toolchain on PATH; formal checks need the pinned Gobra Docker image (shown
by `doctor`) or a compatible local Java/jar/Z3 installation. Images use the exact
digest stored in `internal/pdd/plan.go` and recorded in each receipt.

A clean-checkout smoke test uses an extracted Linux binary to restore committed
example reviews, run actual proofs and HTTP tests, kill mutants and check stale
source handling. It needs Go and Docker with the pinned image already pulled:

```sh
python3 scripts/smoke.py --pdd /absolute/path/to/pdd --version v0.3.0
```

The [release workflow](../.github/workflows/release.yml) builds and validates
artifacts on manual dispatch. It uploads CI artifacts without publishing automatically. Validated output can be
attached to a versioned GitHub release after the source commit passes required
checks. Release publication never changes repository access.
