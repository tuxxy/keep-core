#!/usr/bin/env python3
"""Write a V-04 source manifest pinning actual file bytes for one subject.

Groups follow check-freshness.py: files (production and named tests the
claims rest on), dependency_files (exact SNOWFALL client archive),
verification_files (model, harness, replay tests, docs) and external (tool
binaries and the C-04 contract sources the replay deploys). Usage:
  V04_OUT=<evidence dir> FROST_K03_TBTC=<tbtc-v2> python3 pin.py
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
external = {}
for p in sorted((TB / 'solidity/contracts/frost').glob('*.sol')) + [TB / 'solidity/contracts/bridge/Bridge.sol', TB / 'solidity/contracts/test/FrostPublicFundingRegistryFixture.sol']:
    external[str(p)] = h(p)
for key in ['SNOWFALL_WORKER', 'FROST_K04_BITCOIND', 'FROST_K03_ANVIL']:
    if os.environ.get(key):
        external[os.environ[key]] = h(pathlib.Path(os.environ[key]))
head = subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip()
tb_head = subprocess.check_output(['git', '-C', str(TB), 'rev-parse', 'HEAD'], text=True).strip()
manifest = {'head': head, 'branch': subprocess.check_output(['git', '-C', str(ROOT), 'branch', '--show-current'], text=True).strip(),
            'subject': 'K-05 local redemption host above K-04; C-04 tbtc-v2 ' + tb_head,
            'tbtc_v2_head': tb_head, 'files': files, 'dependency_files': dependency, 'dependency_head': 'c1847ebdf133d0570643ff968539dd498069f69f',
            'dependency_resolution': 'git archive of exact go.mod pin; private modfile only', 'verification_files': verification, 'external': external}
OUT.mkdir(parents=True, exist_ok=True)
(OUT / 'source-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
print(json.dumps({k: len(v) if isinstance(v, dict) else v for k, v in manifest.items()}, indent=1))
