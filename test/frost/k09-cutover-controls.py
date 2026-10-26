import importlib.util,json,os,tempfile,shutil,time,sys
from pathlib import Path
sp=importlib.util.spec_from_file_location('k09',str(Path(__file__).with_name('k09-local.py')));m=importlib.util.module_from_spec(sp);sp.loader.exec_module(m)
os.umask(0o077);env=m.local_environment(os.environ);env['NODE_OPTIONS']='--max-old-space-size=8192';tb=Path(env['FROST_K03_TBTC'])/'solidity'
root=Path(sys.argv[1]).resolve();root.mkdir(mode=0o700,parents=True,exist_ok=False)
r={'status':'running','limits':{'wallSecondsPerCommand':900,'goCores':8},'kind':'focused guard control, not final campaign'}
def save():(root/'result.json').write_text(json.dumps(r,indent=2))
save()
hh=str(tb/'node_modules/.bin/hardhat')
c=m.run_command(m.hardhat_test(hh,'test/bridge/Frost.Movement.test.ts','--grep','uncommitted post-cutover'),tb,env,root/'positive.log',timeout=900)
m.require_exit(c);m.require_mocha((root/'positive.log').read_text(),6)
r['positiveCases']=6;save()
with tempfile.TemporaryDirectory(prefix='k09-r4-timeout-') as tmp:
 d=Path(tmp);shutil.copytree(tb/'contracts',d/'contracts');(d/'node_modules').symlink_to(tb/'node_modules',target_is_directory=True);shutil.copyfile(tb/'tsconfig.json',d/'tsconfig.json');shutil.copyfile(tb/'package.json',d/'package.json')
 p=d/'contracts/frost/FrostMovement.sol';s=p.read_text();old='bool noRoute = self.frostExposure.creationMode';assert s.count(old)==1;p.write_text(s.replace(old,'bool noRoute = false && self.frostExposure.creationMode'));r['mutation']={'file':'contracts/frost/FrostMovement.sol','before':old,'after':'bool noRoute = false && self.frostExposure.creationMode'};save()
 cfg=d/'hardhat.control.config.ts';cfg.write_text('import config from '+json.dumps(str(tb/'hardhat.k09.config'))+';\nconfig.paths={...config.paths,root:__dirname,sources:__dirname+"/contracts",tests:'+json.dumps(str(tb/'test'))+',cache:__dirname+"/cache",artifacts:__dirname+"/build"};\nconfig.typechain={...config.typechain,outDir:__dirname+"/typechain"};\nexport default config;\n')
 hh=str(tb/'node_modules/.bin/hardhat')
 c=m.run_command([hh,'--config',str(cfg),'compile','--force'],d,env,root/'compile.log',timeout=900);m.require_exit(c)
 c=m.run_command([hh,'--config',str(cfg),'test','--no-compile',str(tb/'test/bridge/Frost.Movement.test.ts'),'--grep','uncommitted post-cutover'],d,env,root/'mutant.log',timeout=900)
 output=(root/'mutant.log').read_text();m.require_public_output(output)
 assert c!=0 and 'control LegacyTimeoutRecovery assertion' in output and 'before all' not in output
 r.update(status='pass',guard='LegacyTimeoutRecovery',outcome='killed at owning assertion',exitCode=c);save();print(r)
