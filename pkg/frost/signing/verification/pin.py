#!/usr/bin/env python3
"""Write a V-04 source manifest pinning actual file bytes for one subject.

Groups follow check-freshness.py: files (production and named tests the
claims rest on), dependency_files (exact SNOWFALL client archive),
verification_files (model, harness, replay tests, docs) and external (tool
binaries) and git_blobs (the C-04 contract sources the replay deploys and
the specifications, pinned by git blob in their own repositories so the pin
stays checkable after a worktree is removed). Usage:
  V04_OUT=<evidence dir> FROST_K03_TBTC=<tbtc-v2> python3 pin.py
V04_SPEC and V04_SPEC_REV02 select the spec repositories (defaults below).
"""
import hashlib, json, os, pathlib, subprocess
ROOT = pathlib.Path(__file__).resolve().parents[4]
V = ROOT / 'pkg/frost/signing/verification'
OUT = pathlib.Path(os.environ['V04_OUT'])
TB = pathlib.Path(os.environ['FROST_K03_TBTC'])
def h(p): return hashlib.sha256(p.read_bytes()).hexdigest()
def rel(globs, base=ROOT):
    out = {}
    for g in globs:
        for p in sorted(base.glob(g)):
            if p.is_file():
                out[str(p.relative_to(ROOT)) if base == ROOT else str(p)] = h(p)
    return out
files = rel(['go.mod', 'go.sum', 'pkg/bitcoin/taproot_transaction*.go', 'pkg/bitcoin/transaction_builder*.go', 'pkg/bitcoin/testdata/*',
             'pkg/frost/signing/*.go', 'pkg/frost/signing/INTENT-V2.md', 'pkg/frost/signing/README.md', 'pkg/frost/signing/testdata/*',
             'pkg/frost/snowfallengine/*.go', 'pkg/frost/store/*.go', 'pkg/frost/redemption/*', 'pkg/frost/integration/*.go',
             'pkg/maintainer/spv/frost_redemptions.go', 'pkg/tbtcpg/frost_redemptions.go', 'pkg/tbtc/redemption.go',
             'pkg/chain/ethereum/frost_redemption.go', 'pkg/chain/ethereum/frostbridgeabi/bridge.go', 'pkg/bitcoin/electrum/electrum.go',
             'test/frost/deploy.cjs', 'test/frost/check-k04-intent.ts', 'test/frost/K05.md'])
files = {k: v for k, v in files.items() if '/verification/' not in k}
verification = rel(['pkg/frost/signing/verification/*.md', 'pkg/frost/signing/verification/*.py', 'pkg/frost/signing/verification/*.json',
                    'pkg/frost/signing/verification/model/Cargo.*', 'pkg/frost/signing/verification/model/rust-toolchain.toml',
                    'pkg/frost/signing/verification/model/src/**/*.rs', 'pkg/frost/signing/verification/model/src/*.rs'])
dependency = rel(['pkg/frost/signing/verification/dependency-source/clients/go/*.go'])
def git(repo, *args): return subprocess.check_output(['git', '-C', str(repo), *args], text=True).strip()
def blob(repo, commit, path, track, worktree=None):
    # track is the ref freshness follows: the pin is stale once track:path differs.
    b = git(repo, 'rev-parse', f'{commit}:{path}')
    # The run used the worktree bytes: they must be exactly the pinned blob.
    if worktree is not None and git(worktree, 'hash-object', path) != b:
        raise SystemExit(f'{worktree}/{path} differs from {commit}:{path}')
    return {'repo': git(repo, 'rev-parse', '--path-format=absolute', '--git-common-dir'), 'commit': git(repo, 'rev-parse', commit), 'track': track, 'path': path, 'blob': b}
git_blobs = {}
tb_commit = git(TB, 'rev-parse', 'HEAD')
TB_TRACK = os.environ.get('V04_TBTC_TRACK', 'tuxxy/integration/frost-base')
for p in sorted((TB / 'solidity/contracts/frost').glob('*.sol')) + [TB / 'solidity/contracts/bridge/Bridge.sol', TB / 'solidity/contracts/test/FrostPublicFundingRegistryFixture.sol']:
    path = str(p.relative_to(TB))
    git_blobs['tbtc-v2:' + path] = blob(TB, tb_commit, path, TB_TRACK, TB)
SPEC = pathlib.Path(os.environ.get('V04_SPEC', '/Users/tux/threshold/Research/SNOWFALL/FROST-migration-spec'))
SPEC2 = pathlib.Path(os.environ.get('V04_SPEC_REV02', '/Users/tux/threshold/Research/SNOWFALL/FROST-migration-spec-kcver02'))
for label, repo, commit in [('spec-rev01', SPEC, 'origin/main'), ('kc-ver-rev02', SPEC2, 'kc-ver-02')]:
    for path in ['keep-core-integration-spec.md', 'keep-core-verification-spec.md']:
        git_blobs[f'{label}:{path}'] = blob(repo, commit, path, commit)
external = {}
for key in ['SNOWFALL_WORKER', 'FROST_K04_BITCOIND', 'FROST_K03_ANVIL']:
    if os.environ.get(key):
        external[os.environ[key]] = h(pathlib.Path(os.environ[key]))
head = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
tb_head = subprocess.check_output(['git', '-C', str(TB), 'rev-parse', 'HEAD'], text=True).strip()
manifest = {'head': head, 'branch': subprocess.check_output(['git', '-C', str(ROOT), 'branch', '--show-current'], text=True).strip(),
            'subject': 'keep-core ' + head + '; tbtc-v2 ' + tb_head,
            'tbtc_v2_head': tb_head, 'files': files, 'dependency_files': dependency, 'dependency_head': 'c1847ebdf133d0570643ff968539dd498069f69f',
            'dependency_resolution': 'git archive of exact go.mod pin; private modfile only', 'verification_files': verification, 'external': external, 'git_blobs': git_blobs}
OUT.mkdir(parents=True, exist_ok=True)
(OUT / 'source-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
print(json.dumps({k: len(v) if isinstance(v, dict) else v for k, v in manifest.items()}, indent=1))
