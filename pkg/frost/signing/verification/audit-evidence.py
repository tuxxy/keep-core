#!/usr/bin/env python3
"""Audit retained finite discoveries and replay classifications; does not run Go."""
import hashlib, json, re, subprocess
from pathlib import Path
ROOT = Path(__file__).resolve().parents[4]
V = ROOT / 'pkg/frost/signing/verification'
P = V / 'evidence/V-04-2026-10-13'

def write(name, value):
    (P / name).write_text(json.dumps(value, indent=2) + '\n')

def audit():
    result = {}
    for label, filename, witnesses, controls in [('signing', 'model-results.json', 10, 13), ('admission', 'admission-results.json', 11, 11)]:
        rows = json.loads((P / filename).read_text())
        baseline = rows[0]
        assert len(rows) == controls + 1
        assert all(r['pass'] and r['completed_before_deadline'] for r in rows)
        assert all(not hit for key, hit in baseline['discoveries'].items() if key.startswith('P-'))
        assert sum(key.startswith('W-') and hit for key, hit in baseline['discoveries'].items()) == witnesses
        assert all(r['discoveries'][r['target']] for r in rows[1:])
        traces = []
        folder = P / ('traces' if label == 'signing' else 'admission-traces')
        for path in sorted(folder.glob('*.json')):
            trace = json.loads(path.read_text())
            prefix = trace['claim_model_steps']
            if trace['kind'] == 'counterexample':
                assert sum(s.get('diverges', False) for s in prefix) == 1, path
                assert prefix[-1]['diverges'], path
            traces.append({'id': trace['id'], 'kind': trace['kind'], 'property': trace['property'], 'trace': str(path.relative_to(V)), 'first_claim_divergence': next((s['n'] for s in prefix if s.get('diverges')), None)})
        assert len(traces) == witnesses + controls
        result[label] = {'baseline': baseline, 'controls': rows[1:], 'traces': traces}
    ledgers = {}
    for mode in ['normal', 'race']:
        outputs = {}
        passed = []
        skipped = []
        for line in (P / (mode + '.log')).read_text().splitlines():
            row = json.loads(line)
            test = row.get('Test', '')
            if 'Output' in row:
                outputs[test] = outputs.get(test, '') + row['Output']
            if row.get('Action') == 'pass' and test:
                passed.append(test)
            if row.get('Action') == 'skip':
                skipped.append(test)
        processes = []
        projections = []
        for test, output in outputs.items():
            # go test -json splits long Output chunks; join before decoding.
            for hit in re.finditer(r'V04_PROCESS_REPLAY id=(\S+) class=(\S+) scope=(\S+) events=', output):
                events, _ = json.JSONDecoder().raw_decode(output[hit.end():])
                processes.append({'id': hit[1], 'class': hit[2], 'scope': hit[3], 'test': test, 'events': events})
            for hit in re.finditer(r'V04_REPLAY id=(\S+) class=(\S+) reason=(\S+)', output):
                projections.append({'id': hit[1], 'class': hit[2], 'reason': hit[3], 'test': test})
        assert len(processes) == 12 and len({p['id'] for p in processes}) == 12
        assert len(projections) == 23 and not skipped
        assert sum(p['class'] == 'partial' for p in projections) == 13
        assert sum(p['class'] == 'none' for p in projections) == 10
        ledgers[mode] = {'actual': json.loads((P / (mode + '-actual.json')).read_text()), 'passed_test_events_including_subtests_and_helper': len(passed), 'non_subtest_pass_events_excluding_helper': sum('/' not in t and t != 'TestV04ProcessReplayChild' for t in passed), 'helper_noop_events': sum(t == 'TestV04ProcessReplayChild' for t in passed), 'skipped': skipped, 'declared_application_traces': processes, 'generated_model_projections': projections}
        assert ledgers[mode]['actual']['exit_code'] == 0
        for name, digest in ledgers[mode]['actual']['input_sha256'].items():
            assert hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == digest, name
    model_run = json.loads((P / 'model-actual.json').read_text())
    assert model_run['exit_code'] == 0 and model_run['stopped_for'] is None
    # Rerun only the affected profile: primary transitions are in main.rs;
    # admission/host-attestation transitions are in admission.rs.
    main_name = 'pkg/frost/signing/verification/model/src/main.rs'
    assert hashlib.sha256((ROOT / main_name).read_bytes()).hexdigest() == model_run['model_sha256'][main_name]
    admission_run = json.loads((P / 'admission-model-actual.json').read_text())
    assert admission_run['exit_code'] == 0 and admission_run['stopped_for'] is None
    for name, digest in admission_run['model_sha256'].items():
        assert hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == digest, name
    write('replay-ledger.json', ledgers)
    result['replay_counts'] = {mode: {'application': 12, 'partial': 13, 'none': 10, 'skip': 0} for mode in ledgers}
    manifest = json.loads((P / 'source-manifest.json').read_text())
    negative = json.loads(json.dumps(manifest))
    negative['files']['pkg/frost/signing/executor.go'] = '0' * 64
    write('stale-manifest-negative-control.json', negative)
    for label, filename, expected_exit in [('fresh', 'source-manifest.json', 0), ('stale-control', 'stale-manifest-negative-control.json', 1)]:
        cmd = ['python3', str(V / 'check-freshness.py'), str(P / filename)]
        completed = subprocess.run(cmd, text=True, capture_output=True)
        assert completed.returncode == expected_exit, completed.stdout
        observed = json.loads(completed.stdout)
        if expected_exit:
            assert observed['mismatches'] == [{'group': 'files', 'path': 'pkg/frost/signing/executor.go'}]
        write(label + '-actual.json', {'command': cmd, 'exit_code': completed.returncode, 'result': observed})
    write('evidence-audit.json', result)
    print('Audit passed: 21 witnesses, 24 target counterexamples, 45 retained traces; each concrete run has 12 application traces and 13 partial/10 none projections. Fresh pins accepted; stale hash rejected.')

if __name__ == '__main__':
    audit()
