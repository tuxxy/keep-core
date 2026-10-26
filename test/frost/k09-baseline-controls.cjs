// Bounded baseline binding probes, no chains, keys or operator records.
const fs=require('fs'),os=require('os'),path=require('path'),vm=require('vm'),assert=require('assert');
const filename=path.join(__dirname,'k09-baseline.cjs');
const original=fs.readFileSync(filename,'utf8');
function probe(source){
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'k09-baseline-control-'));
 try{
  const build=path.join(root,'build'),sourceName='contracts/bridge/Bridge.sol';
  const folder=path.join(build,sourceName);fs.mkdirSync(folder,{recursive:true});
  const file=path.join(folder,'Bridge.json'),info=path.join(folder,'info.json');
  fs.writeFileSync(file,JSON.stringify({sourceName,abi:[],bytecode:'0x00',deployedBytecode:'0x00'}));
  fs.writeFileSync(path.join(folder,'Bridge.dbg.json'),JSON.stringify({buildInfo:'info.json'}));
  fs.writeFileSync(info,JSON.stringify({solcVersion:'0.8.17',input:{sources:{[sourceName]:{content:'original'}}},output:{contracts:{[sourceName]:{Bridge:{abi:[],evm:{bytecode:{object:'00'},deployedBytecode:{object:'00'}}}}}}}));
  const module={exports:{}};
  const localRequire=name=>name==='child_process'?{execFileSync:(command,args)=>{assert.equal(command,'git');assert.equal(args[1],'76f790511:solidity/'+sourceName);return Buffer.from('original')}}:require(name);
  vm.runInNewContext(source,{require:localRequire,module,__dirname,__filename:filename,process,Buffer,console},{filename,timeout:1000});
  const binding=()=>module.exports.baselineArtifacts(root,build).read(sourceName,'Bridge');
  binding(); // unchanged positive witness
  fs.writeFileSync(file,JSON.stringify({sourceName,abi:[],bytecode:'0x01',deployedBytecode:'0x00'}));
  assert.throws(binding,/baseline output mismatch/,'control BaselineArtifact assertion');
 }finally{fs.rmSync(root,{recursive:true,force:true})}
}
probe(original);
const needle="if(artifact[key]!=='0x'+compiled.evm[key].object)";
assert(original.includes(needle));
assert.throws(()=>probe(original.replace(needle,'if(false)')),e=>e.code==='ERR_ASSERTION'&&e.message.includes('control BaselineArtifact assertion'));
// An unrelated harness outage must not be classified as a guard kill.
assert.throws(()=>probe('throw Error("harness outage")'),/harness outage/);
console.log('BaselineArtifact: unchanged witness passes; weakened guard fails at owning assertion; harness outage remains error');
