#!/usr/bin/env python3
"""Compare the opt-in mixed-load fixture on the host and in a local Docker app.

Owns a PostgreSQL container, a separate application container, a named database
volume and a bridge network. It never opens the operating config or a real node.
All binaries are built before timing; raw logs and summaries survive cleanup.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import statistics
import subprocess
import time
import uuid

LABEL = 'com.0xmhha.indexer.api-placement'


def command(args, **kwargs):
    kwargs.setdefault('timeout', 60)
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout.strip()


def owned(kind, name, owner):
    args = ['docker', 'inspect', name] if kind == 'container' else ['docker', kind, 'inspect', name]
    result = subprocess.run(args, text=True, capture_output=True)
    if result.returncode:
        if 'no such' in result.stderr.lower() or 'not found' in result.stderr.lower():
            return False
        raise RuntimeError(result.stderr.strip())
    item = json.loads(result.stdout)[0]
    labels = item['Config']['Labels'] if kind == 'container' else item.get('Labels')
    if (labels or {}).get(LABEL) != owner:
        raise RuntimeError('refusing cleanup: ownership mismatch for ' + name)
    return True


def logged(args, env, cwd, path):
    with path.open('w') as log:
        child = subprocess.Popen(args, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
        try:
            status = child.wait(timeout=480)
        finally:
            if child.poll() is None:
                child.terminate()
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()
    if status:
        raise RuntimeError('command failed; see ' + str(path))


class MeasurementError(RuntimeError):
    """Failed runs retain their parsed diagnostics, but are never accepted."""

    def __init__(self, path, reasons, row):
        self.row = row
        self.reasons = reasons
        super().__init__('invalid measurement (' + ', '.join(reasons) + '); see ' + str(path))


def parse_measurement(text, path, command_error=None):
    reasons, row = [], None
    try:
        rows = [json.loads(line.split(' ', 1)[1]) for line in text.splitlines()
                if line.startswith('MIXED_POOL_MEASURE ')]
        if len(rows) != 1 or not isinstance(rows[0], dict):
            reasons.append('missing_or_multiple_results')
        else:
            row = rows[0]
    except (ValueError, TypeError):
        reasons.append('invalid_result_json')
    if command_error is not None:
        reasons.append('command_failed')
    if 'PASS' not in text.splitlines():
        reasons.append('missing_PASS')
    if row is not None:
        api = row.get('api')
        if not isinstance(api, dict) or not api:
            reasons.append('missing_api_results')
        elif any(not isinstance(v, dict) or v.get('Errors') for v in api.values()):
            reasons.append('api_errors')
        elif any(v.get('IncompleteDiagnostics', 0) for v in api.values()):
            reasons.append('incomplete_api_diagnostics')
        if 'apiValidation' in row:
            verdict = row['apiValidation']
            if not isinstance(verdict, dict) or verdict.get('Passed') is not True:
                reasons.append('api_validation_failed')
    if reasons:
        # The raw log is untouched, even when Go fails AFTER emitting its JSON.
        path.with_suffix('.failed.json').write_text(json.dumps(
            dict(valid=False, reasons=reasons, row=row), indent=2))
        raise MeasurementError(path, reasons, row)
    return row


def measured(args, env, cwd, path):
    command_error = None
    try:
        logged(args, env, cwd, path)
    except Exception as error:
        command_error = error
    return parse_measurement(path.read_text() if path.exists() else '', path, command_error)


def verify(row, blocks, block_list_queries=1):
    height = blocks + 2
    assert row['maxConns'] == 4 and row['apiReservedReadSlots'] == 1
    assert row['timedBlocks'] == blocks and row['warmBlocks'] == 2
    assert row['transactions'] == height * 2 and row['logs'] == height * 16
    assert row['outboxEvents'] == height * 19
    for stage in ('source_validate', 'source_ack'):
        assert row['pipeline'][stage]['count'] == height and row['pipeline'][stage]['errors'] == 0
    capture = row['runtime']['metrics']['captureCommit']
    assert capture['count'] == height and capture['failures'] == 0
    for route, api in row['api'].items():
        assert not api['Errors']
        queries = 1 if route.endswith('/block') else block_list_queries
        assert api['Queries'] == {str(queries): api['Requests']}
        assert not api.get('IncompleteDiagnostics', 0)
    if 'apiValidation' in row:
        assert row['apiValidation']['Passed'] is True
        assert not row['apiValidation']['Errors']
        assert not row['apiValidation']['IncompleteDiagnostics']
    assert 0 < row['physicalRequests'] <= row['logicalRPC']['eth_getBlockByNumber']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True, help='new directory for raw logs and results')
    parser.add_argument('--blocks', type=int, choices=(4, 32, 128), default=128)
    parser.add_argument('--rounds', type=int, choices=range(1, 10), default=3)
    parser.add_argument('--max-host-load', type=float,
                        help='optional maximum one-minute host load at startup and before/after each run; does not isolate CPU, memory or IO')
    opts = parser.parse_args()
    if opts.max_host_load is not None and (not math.isfinite(opts.max_host_load) or opts.max_host_load <= 0):
        parser.error('--max-host-load must be finite and positive')
    repo = Path(__file__).resolve().parents[1]
    out = opts.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    load_samples = []

    def check_host_load(phase, **details):
        sample = dict(phase=phase, timeUnix=time.time(), logicalCPUs=os.cpu_count(),
                      loadAverage=os.getloadavg(), limit=opts.max_host_load, **details)
        load_samples.append(sample)
        (out / 'host-load.json').write_text(json.dumps(load_samples, indent=2))
        if opts.max_host_load is not None and sample['loadAverage'][0] > opts.max_host_load:
            raise RuntimeError('host load exceeds --max-host-load; incomplete results retained in ' + str(out))

    # Reject a saturated host before inspecting Docker or creating resources.
    # This is a screening condition, not a claim of stable or isolated hardware.
    check_host_load('startup')
    context = os.environ.get('DOCKER_CONTEXT')
    endpoint = command(['docker', 'context', 'inspect', context, '--format',
                        '{{.Endpoints.docker.Host}}']) if context else (
        os.environ.get('DOCKER_HOST') or command(
            ['docker', 'context', 'inspect', '--format', '{{.Endpoints.docker.Host}}']))
    if not endpoint.startswith('unix://'):
        raise RuntimeError('this comparison requires a local Docker Unix socket')
    owner = 'indexer-api-placement-' + uuid.uuid4().hex[:16]
    db, app, network, volume = (owner + suffix for suffix in ('-db', '-app', '-net', '-data'))
    label = LABEL + '=' + owner
    native, linux = out / 'native.test', out / 'linux.test'
    inventory = [{key: row[key] for key in ('ID', 'Names', 'State', 'Status')}
                 for row in (json.loads(line) for line in command(
                     ['docker', 'ps', '-a', '--format', 'json']).splitlines())]
    (out / 'owned.json').write_text(json.dumps(dict(owner=owner, db=db, app=app, network=network, volume=volume), indent=2))
    cleanup_errors = []
    try:
        command(['docker', 'network', 'create', '--label', label, network])
        command(['docker', 'volume', 'create', '--label', label, volume])
        command(['docker', 'run', '-d', '--name', db, '--label', label, '--network', network,
                 '--mount', f'type=volume,src={volume},dst=/var/lib/postgresql/data',
                 '-e', 'POSTGRES_USER=indexer', '-e', 'POSTGRES_HOST_AUTH_METHOD=trust',
                 '-p', '127.0.0.1::5432', 'postgres:16-alpine'])
        for _ in range(100):
            ready = subprocess.run(['docker', 'exec', db, 'psql', '-h', '127.0.0.1', '-U', 'indexer', '-Atc', 'SHOW fsync'], capture_output=True, text=True, timeout=10)
            if ready.returncode == 0 and ready.stdout.strip() == 'on':
                break
            time.sleep(.3)
        else:
            raise RuntimeError('durable PostgreSQL TCP server did not start')
        port = command(['docker', 'port', db, '5432/tcp']).rsplit(':', 1)[1]
        image_id = command(['docker', 'inspect', '--format', '{{.Image}}', db])
        arch = command(['docker', 'image', 'inspect', '--format', '{{.Architecture}}', image_id])
        (out / 'environment.json').write_text(json.dumps(
            dict(imageID=image_id, architecture=arch, blocks=opts.blocks, rounds=opts.rounds,
                 gomaxprocs=os.environ.get('GOMAXPROCS'), godebug=os.environ.get('GODEBUG')), indent=2))
        build_info = {}
        for stage, binary in (('native', native), ('docker', linux)):
            env = dict(os.environ, CGO_ENABLED='0')
            if stage == 'docker':
                env.update(GOOS='linux', GOARCH=arch)
            else:
                env.pop('GOOS', None)
                env.pop('GOARCH', None)
            logged(['go', 'test', '-c', '-o', str(binary), './pkg/app'],
                   env, repo, out / ('build-' + stage + '.log'))
            build_info[stage] = dict(sha256=hashlib.sha256(binary.read_bytes()).hexdigest(), info=command(['go', 'version', '-m', str(binary)]))
        (out / 'build-info.json').write_text(json.dumps(build_info, indent=2))
        (out / 'source-sha256.json').write_text(json.dumps({
            name: hashlib.sha256((repo / name).read_bytes()).hexdigest()
            for name in ('go.mod', 'go.sum', 'pkg/app/mixed_pool_measure_test.go',
                         'pkg/storage/postgres/query_reader.go', 'pkg/fetch/writer.go')
        }, indent=2))
        command(['docker', 'run', '-d', '--name', app, '--label', label, '--network', network,
                 '--mount', 'type=tmpfs,dst=/var/lib/postgresql/data', '--entrypoint', 'sleep', 'postgres:16-alpine', 'infinity'])
        command(['docker', 'cp', str(linux), app + ':/tmp/indexer-measure.test'])
        command(['docker', 'exec', app, 'chmod', '755', '/tmp/indexer-measure.test'])
        runs = []
        for mode, interval in (('backlog', 1), ('streaming', 500)):
            for number in range(1, opts.rounds + 1):
                for stage in (('native', 'docker') if number % 2 else ('docker', 'native')):
                    check_host_load('before-run', mode=mode, round=number, stage=stage)
                    values = dict(INDEXER_TEST_DRIVER='postgres', INDEXER_MEASURE_MIXED_POOL='1', INDEXER_MEASURE_MIXED_CONNS='4', INDEXER_MEASURE_MIXED_BLOCKS=str(opts.blocks), INDEXER_MEASURE_MIXED_INTERVAL_MS=str(interval))
                    host = '127.0.0.1:' + port if stage == 'native' else db + ':5432'
                    values['INDEXER_TEST_POSTGRES'] = f'postgres://indexer@{host}/indexer?default_query_exec_mode=simple_protocol&synchronous_commit=off'
                    flags = ['-test.v', '-test.run=^TestMeasureManagedFeedPoolLoad$', '-test.timeout=6m']
                    if stage == 'native':
                        args, env, cwd = [str(native), *flags], {**os.environ, **values}, repo / 'pkg/app'
                    else:
                        args = ['docker', 'exec', '-w', '/tmp']
                        for key, value in values.items():
                            args.extend(('-e', key + '=' + value))
                        for key in ('GOMAXPROCS', 'GODEBUG'):
                            if key in os.environ:
                                args.extend(('-e', key + '=' + os.environ[key]))
                        args.extend((app, '/tmp/indexer-measure.test', *flags))
                        env, cwd = os.environ, repo
                    path = out / f'{mode}-round{number}-{stage}.log'
                    row = measured(args, env, cwd, path)
                    verify(row, opts.blocks)
                    row.update(mode=mode, stage=stage, round=number)
                    runs.append(row)
                    (out / 'runs.json').write_text(json.dumps(runs, indent=2))
                    check_host_load('after-run', mode=mode, round=number, stage=stage)
                    print(path.name + ' passed', flush=True)
        assert len({row['outputDigest'] for row in runs}) == 1
        summaries = []
        for mode in ('backlog', 'streaming'):
            for stage in ('native', 'docker'):
                group = [row for row in runs if row['mode'] == mode and row['stage'] == stage]
                cohort = 'drain' if mode == 'backlog' else 'injection'
                summaries.append(dict(mode=mode, stage=stage, cohort=cohort,
                    point={p: statistics.median(row['api'][cohort + '/block']['AllMS'][p] for row in group) for p in ('P50', 'P95', 'P99')},
                    pointRequests=[row['api'][cohort + '/block']['Requests'] for row in group],
                    blocksPerSecond=statistics.median(row['blocksPerSecond'] for row in group)))
        (out / 'measurements.json').write_text(json.dumps(dict(method='median of run percentiles; same source and CGO0; host/runtime/network/raw-storage placement comparison, not isolated network causality or an operational SLO', summary=summaries, runs=runs), indent=2))
    finally:
        for kind, name, args in (
            ('container', app, ['docker', 'rm', '-fv', app]),
            ('container', db, ['docker', 'rm', '-fv', db]),
            ('network', network, ['docker', 'network', 'rm', network]),
            ('volume', volume, ['docker', 'volume', 'rm', volume])):
            try:
                if owned(kind, name, owner):
                    command(args)
            except Exception as error:
                cleanup_errors.append(str(error))
        for binary in (native, linux):
            binary.unlink(missing_ok=True)
        (out / 'cleanup.json').write_text(json.dumps(dict(errors=cleanup_errors, existingContainersBefore=inventory), indent=2))
        if cleanup_errors:
            raise RuntimeError('cleanup failed: ' + '; '.join(cleanup_errors))


if __name__ == '__main__':
    main()
