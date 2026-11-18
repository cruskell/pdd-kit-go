#!/usr/bin/env python3
"""Exercise an installed pdd binary against a clean clone and real Gobra."""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pdd", required=True, type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--policy", type=Path, help="independently provisioned public authority policy for the signed validation example")
    args = parser.parse_args()
    binary = args.pdd.resolve()
    with tempfile.TemporaryDirectory(prefix="pdd-clean-checkout-") as directory:
        clone = Path(directory) / "repo"
        subprocess.run(["git", "clone", "--no-hardlinks", "--quiet", str(ROOT), str(clone)], check=True)

        def cli(*command, success=True):
            result = subprocess.run([str(binary), "--cwd", str(clone), "--json", *command], capture_output=True, text=True)
            try:
                data = json.loads(result.stdout)
            except ValueError as error:
                raise RuntimeError(f"invalid command output: {result.stdout}\n{result.stderr}") from error
            assert data["schema"] == 1 and data["exit_code"] == result.returncode
            assert data["ok"] == (result.returncode == 0) == success, (command, data, result.stderr)
            return data["data"]

        assert cli("--version")["version"] == args.version
        cli("doctor")
        for plan in ("default", "httpwallet"):
            if plan == "httpwallet":
                cli("--plan", plan, "init", "--from", "examples/httpwallet/plan.json")
            review = json.loads((clone / f".pdd/reviews/{plan}.json").read_text())
            cli("--plan", plan, "import-approval", "--from", f".pdd/reviews/{plan}.json")
            result = cli("--plan", plan, "prove")
            assert result["current"] and result["receipt"]["success"]
            assert result["receipt"]["approval"] == review, "CI changed a saved review"
            assert all(m["status"] == "killed" for m in result["receipt"]["mutations"])
            result = cli("--plan", plan, "status")
            assert result["current"] and result["receipt"]["success"]
            assert cli("--plan", plan, "tasks")["machine_success"]
            assert cli("--plan", plan, "decisions")["open_decisions"] == []
            dashboard = f".pdd/{plan}-review.html"
            cli("--plan", plan, "report", "--out", dashboard)
            assert "Frozen approval contract" in (clone / dashboard).read_text()
            assert "Proof-Driven Development receipt" in cli("--plan", plan, "pr")["body"]
            print(f"{plan}: preserved review, real proof, empirical checks, mutants and freshness passed", flush=True)
        if args.policy:
            signed = ("--plan", "validation", "--policy", str(args.policy.resolve()))
            cli(*signed, "init", "--from", "examples/validation/plan.json")
            review = json.loads((clone / ".pdd/reviews/validation-signed.json").read_text())
            cli(*signed, "import-approval", "--from", ".pdd/reviews/validation-signed.json")
            result = cli(*signed, "prove")
            assert result["current"] and result["receipt"]["success"]
            assert result["receipt"]["approval"] == review
            assert result["receipt"]["approval_policy"]["key_id"]
            assert cli(*signed, "status")["current"]
            cli("--plan", "validation", "status", success=False)
            print("validation: installed binary enforced the signed authority policy", flush=True)
        # A later edit must fail even if the historical receipt says success.
        path = clone / "examples/httpwallet/app/handler.go"
        path.write_text(path.read_text() + "\n// clean-checkout stale-source probe\n")
        result = cli("--plan", "httpwallet", "status", success=False)
        assert not result["current"]
        print("source edits invalidate installed-binary evidence", flush=True)


if __name__ == "__main__":
    main()
