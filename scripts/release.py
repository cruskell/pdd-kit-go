#!/usr/bin/env python3
"""Build deterministic CLI archives from a clean checkout using the pinned Go."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import zipfile

TOOLCHAIN = "go1.27.4"
TARGETS = [("linux", "amd64"), ("linux", "arm64"), ("darwin", "amd64"),
           ("darwin", "arm64"), ("windows", "amd64")]
ROOT = Path(__file__).resolve().parent.parent


def run(*args, **kwargs):
    return subprocess.check_output(args, cwd=ROOT, text=True, **kwargs).strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="version such as v0.1.0 or v0.1.0-rc.1")
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", args.version):
        parser.error("use a versioned release identifier")
    if run("git", "status", "--porcelain", "--untracked-files=all"):
        parser.error("release builds require a clean checkout (commit changes first)")
    go = os.environ.get("GO", "go")
    env = dict(os.environ, GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="", CGO_ENABLED="0",
               GOEXPERIMENT="", GOFIPS140="off", GOAMD64="v1", GOARM64="v8.0")
    if run(go, "env", "GOVERSION", env=env) != TOOLCHAIN:
        parser.error(f"release builds require {TOOLCHAIN}; select it with GO=/path/to/go")
    out = (args.out or ROOT / "dist" / args.version).resolve()
    if out.exists() and any(out.iterdir()):
        parser.error("output directory must be empty; stale artifacts are never reused")
    out.mkdir(parents=True, exist_ok=True)
    commit = run("git", "rev-parse", "HEAD")
    flags = ["-trimpath", "-buildvcs=false", "-ldflags=-s -w -X main.version=" + args.version]
    license_bytes = (ROOT / "LICENSE").read_bytes()
    install = ("pdd-kit-go " + args.version + "\n\n"
               "Verify SHA256SUMS before extracting; place pdd (pdd.exe on Windows) on PATH.\n"
               "Run pdd --version and pdd --help. Go is needed for empirical tests;\n"
               "formal verification needs Docker and the pinned Gobra image (pdd doctor).\n"
               "Source, examples and full instructions:\nhttps://github.com/cruskell/pdd-kit-go\n"
               "Use --policy with authorized signed reviews for production CI; read trust boundaries.\n").encode()
    for system, arch in TARGETS:
        name = "pdd.exe" if system == "windows" else "pdd"
        binary = out / name
        target_env = dict(env, GOOS=system, GOARCH=arch)
        subprocess.run([go, "build", *flags, "-o", str(binary), "./cmd/pdd"], cwd=ROOT, env=target_env, check=True)
        files = [(name, binary.read_bytes(), 0o755), ("LICENSE", license_bytes, 0o644), ("INSTALL.txt", install, 0o644)]
        binary.unlink()
        prefix = f"pdd-kit-go_{args.version}_{system}_{arch}"
        if system == "windows":
            with zipfile.ZipFile(out / (prefix + ".zip"), "w", compression=zipfile.ZIP_STORED) as archive:
                for filename, data, mode in files:
                    entry = zipfile.ZipInfo(prefix + "/" + filename, date_time=(1980, 1, 1, 0, 0, 0))
                    entry.create_system = 3
                    entry.external_attr = (0o100000 | mode) << 16
                    archive.writestr(entry, data)
        else:
            tar = io.BytesIO()
            with tarfile.open(fileobj=tar, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for filename, data, mode in files:
                    entry = tarfile.TarInfo(prefix + "/" + filename)
                    entry.size, entry.mode, entry.mtime = len(data), mode, 0
                    archive.addfile(entry, io.BytesIO(data))
            with (out / (prefix + ".tar.gz")).open("wb") as file:
                # level 0 keeps archives reproducible across zlib versions.
                with gzip.GzipFile(filename="", fileobj=file, mode="wb", compresslevel=0, mtime=0) as archive:
                    archive.write(tar.getvalue())
        print(prefix, flush=True)
    metadata = {"schema": 1, "version": args.version, "commit": commit, "toolchain": TOOLCHAIN,
                "cgo_enabled": False, "flags": flags, "targets": [f"{s}/{a}" for s, a in TARGETS]}
    (out / "BUILD.json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    sums = "".join(f"{hashlib.sha256(file.read_bytes()).hexdigest()}  {file.name}\n"
                   for file in sorted(out.iterdir()))
    (out / "SHA256SUMS").write_text(sums, encoding="ascii")
    print(out)


if __name__ == "__main__":
    main()
