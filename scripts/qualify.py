#!/usr/bin/env python3
"""Repeat a real, signed-policy verification gate and record operational evidence.

This is a finite qualification run, not a claim of production deployment history.
No private key is read and no approval is created by this script.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import platform
import subprocess
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--pdd', type=Path, required=True)
    parser.add_argument('--cwd', type=Path, required=True)
    parser.add_argument('--policy', type=Path, required=True)
    parser.add_argument('--plan', required=True)
    parser.add_argument('--approval', type=Path, required=True)
    parser.add_argument('--iterations', type=int, default=3)
    parser.add_argument('--validation-recovery', action='store_true', help='exercise wrong implementation and recovery in a disposable validation-example clone')
    args = parser.parse_args()
    if not 1 <= args.iterations <= 100:
        parser.error('iterations must be 1..100')
    root = args.cwd.resolve()
    policy = args.policy.resolve()
    approval = args.approval if args.approval.is_absolute() else root / args.approval
    original = json.loads(approval.read_text())
    env = dict(os.environ)
    env.pop('PDD_REVIEW_POLICY', None)
    report = {'schema': 1, 'scenario': 'repository-operational-qualification',
              'production_deployment_history': False, 'platform': platform.platform(),
              'plan': args.plan, 'started_at': datetime.now(timezone.utc).isoformat(), 'runs': [], 'success': False}
    output = root / '.pdd/results/qualification.json'
    output.parent.mkdir(parents=True, exist_ok=True)

    def cli(*command, with_policy=True, success=True):
        prefix = [str(args.pdd.resolve()), '--cwd', str(root), '--plan', args.plan, '--json']
        if with_policy:
            prefix += ['--policy', str(policy)]
        result = subprocess.run(prefix + list(command), capture_output=True, text=True, env=env, timeout=600)
        data = json.loads(result.stdout)
        assert data['exit_code'] == result.returncode and data['ok'] == (result.returncode == 0)
        assert data['ok'] == success, (command, data, result.stderr)
        return data.get('data')

    try:
        imported = cli('import-approval', '--from', str(approval))
        assert imported == original, 'import changed the signed review'
        for index in range(args.iterations):
            started = time.monotonic()
            result = cli('prove')
            assert result['current'] and result['receipt']['success']
            assert result['receipt']['approval'] == original
            assert result['receipt']['approval_policy']['key_id']
            assert all(m['status'] == 'killed' for m in result['receipt']['mutations'])
            current = cli('status')
            assert current['current'] and current['receipt']['success']
            cli('status', with_policy=False, success=False)
            report['runs'].append({'iteration': index + 1, 'seconds': round(time.monotonic() - started, 3),
                                   'receipt': result['path'], 'source_digest': result['receipt']['source_digest'],
                                   'policy': result['receipt']['approval_policy']})
            print(f"{args.plan}: signed gate {index+1}/{args.iterations} passed", flush=True)
        assert len({r['source_digest'] for r in report['runs']}) == 1, 'repository drifted between runs'
        if args.validation_recovery:
            if args.plan != 'validation':
                raise ValueError('recovery probe is restricted to the shipped validation example')
            source = root / 'examples/validation/slice.go'
            original_source = source.read_bytes()
            before, after = b'return true', b'return false'
            assert original_source.count(before) == 1
            try:
                source.write_bytes(original_source.replace(before, after, 1))
                failed = cli('prove', success=False)
                assert failed['current'] and not failed['receipt']['success']
                negative = cli('status', success=False)
                assert negative['current'] and not negative['receipt']['success']
                report['wrong_implementation_rejected'] = True
            finally:
                source.write_bytes(original_source)
            recovered = cli('prove')
            assert recovered['current'] and recovered['receipt']['success']
            report['recovery_verified'] = True
            print('wrong implementation rejected; latest negative receipt observed; recovery passed', flush=True)
        report['success'] = True
    except BaseException as error:
        report['error'] = str(error)
        raise
    finally:
        report['finished_at'] = datetime.now(timezone.utc).isoformat()
        output.write_text(json.dumps(report, indent=2) + '\n')
    print(output)


if __name__ == '__main__':
    main()
