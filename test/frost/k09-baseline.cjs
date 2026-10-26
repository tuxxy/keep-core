// Immutable legacy deployment for the public K-09 precursor. No storage writes.
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const {execFileSync} = require('child_process');
const {ethers} = require(path.join(__dirname, '../../solidity/ecdsa/node_modules/ethers'));
const PIN = '76f790511';
function baselineArtifacts(repository, build = process.env.FROST_C04_LEGACY_BASELINE_BUILD) {
 if (!build) throw Error('pinned legacy baseline build required');
 const infos = new Map(), files = {}, inputs = new Map();
 const digest = bytes => crypto.createHash('sha256').update(bytes).digest('hex');
 function read(source, name) {
  const file=path.join(build,source,name+'.json');
  const artifact=JSON.parse(fs.readFileSync(file));
  const debug=JSON.parse(fs.readFileSync(file.replace(/\.json$/,'.dbg.json')));
  const infoPath=path.resolve(path.dirname(file),debug.buildInfo);
  if(!infos.has(infoPath)) {
   const raw=fs.readFileSync(infoPath), info=JSON.parse(raw);
   if(info.solcVersion!=='0.8.17') throw Error('baseline compiler mismatch');
   for(const [input,value] of Object.entries(info.input.sources)) {
    if(!inputs.has(input)) {
     const bytes=input.startsWith('contracts/') ? execFileSync('git',['show',PIN+':solidity/'+input],{cwd:repository,timeout:10000,maxBuffer:8*1024*1024}) : fs.readFileSync(path.join(repository,'solidity/node_modules',input));
     inputs.set(input,bytes.toString());
    }
    if(inputs.get(input)!==value.content) throw Error('baseline source mismatch: '+input);
   }
   infos.set(infoPath,{info,hash:digest(raw)});
  }
  const {info,hash}=infos.get(infoPath), compiled=info.output.contracts[source][name];
  if(artifact.sourceName!==source || JSON.stringify(artifact.abi)!==JSON.stringify(compiled.abi)) throw Error('baseline artifact identity mismatch');
  for(const key of ['bytecode','deployedBytecode']) if(artifact[key]!=='0x'+compiled.evm[key].object) throw Error('baseline output mismatch: '+source+':'+name);
  if((artifact.deployedBytecode.length-2)/2>24576) throw Error('baseline EIP-170');
  files[file]={sha256:digest(fs.readFileSync(file)),buildInfo:infoPath,buildInfoSha256:hash};
  return artifact;
 }
 return {read,files};
}
async function deployBaseline(repository,signer) {
 const bound=baselineArtifacts(repository), deployed=new Map();
 async function deploy(source,name) {
  const key=source+':'+name;
  if(deployed.has(key)) return deployed.get(key);
  const a=bound.read(source,name);let code=a.bytecode;
  for(const [source,names] of Object.entries(a.linkReferences||{})) for(const [name,positions] of Object.entries(names)) {
   const lib=await deploy(source,name);
   for(const p of positions) code=code.slice(0,2+p.start*2)+lib.address.slice(2)+code.slice(2+(p.start+p.length)*2);
  }
  const contract=await new ethers.ContractFactory(a.abi,code,signer).deploy();await contract.deployed();
  if((await signer.provider.getCode(contract.address)).length/2-1>24576)throw Error('baseline deployed EIP-170');
  deployed.set(key,contract);return contract;
 }
 const implementation=await deploy('contracts/bridge/Bridge.sol','Bridge');
 const bank=await deploy('contracts/bank/Bank.sol','Bank');
 return {implementation,bank,manifest:{revision:PIN,artifacts:bound.files}};
}
module.exports={baselineArtifacts,deployBaseline};

if(require.main===module){
 const bound=baselineArtifacts(process.argv[2],process.argv[3]);
 const pending=[['contracts/bridge/Bridge.sol','Bridge'],['contracts/bank/Bank.sol','Bank']],seen=new Set();
 while(pending.length){const [source,name]=pending.pop(),key=source+':'+name;if(seen.has(key))continue;seen.add(key);
  const a=bound.read(source,name);for(const [file,names]of Object.entries(a.linkReferences||{}))for(const library of Object.keys(names))pending.push([file,library]);
 }
 process.stdout.write(JSON.stringify(bound.files));
}
