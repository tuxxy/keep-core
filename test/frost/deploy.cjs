// Local-only K-03 deployment. Requires compiled, pinned KC and TB worktrees.
// Anvil keeps EIP-170 enabled; no stubs or unlimited-size switch are used.
const fs = require('fs');
const path = require('path');
const { ethers } = require(path.join(__dirname, '../../solidity/ecdsa/node_modules/ethers'));
async function main() {
 const [url, tbRoot, output, patternText] = process.argv.slice(2);
 if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(url)) throw Error('local RPC required');
 const kcRoot = path.resolve(__dirname, '../..');
 const provider = new ethers.providers.JsonRpcProvider(url);
 const signer = provider.getSigner(0);
 const admin = await signer.getAddress();
 const libraries = new Map();
 const artifacts = {};
 function artifact(root, source, name) { return JSON.parse(fs.readFileSync(path.join(root, 'solidity', root===kcRoot?'ecdsa/build':'build', source, name+'.json'))); }
 async function deploy(root, source, name, args=[]) {
  const a = artifact(root,source,name);let bytecode=a.bytecode;
  for(const [file,names] of Object.entries(a.linkReferences||{})) {
   for(const [lib,positions] of Object.entries(names)) {
    const key=root+file+lib;
    if(!libraries.has(key)) libraries.set(key,(await deploy(root,file,lib)).address);
    const address=libraries.get(key).slice(2);
    for(const p of positions) bytecode=bytecode.slice(0,2+p.start*2)+address+bytecode.slice(2+(p.start+p.length)*2);
   }
  }
  const c=await new ethers.ContractFactory(a.abi,bytecode,signer).deploy(...args);await c.deployed();
  const size=(await provider.getCode(c.address)).length/2-1;if(size>24576)throw Error(name+' violates EIP-170');
  artifacts[name]={address:c.address,source,codeHash:ethers.utils.keccak256(await provider.getCode(c.address)),size};
  return c;
 }
 const implementation=await deploy(tbRoot,'contracts/bridge/Bridge.sol','Bridge');
 const proxyAdmin=await deploy(tbRoot,'@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol','ProxyAdmin');
 const initializer=implementation.interface.encodeFunctionData('initialize',[admin,admin,admin,admin,admin,1]);
 const proxy=await deploy(tbRoot,'@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol','TransparentUpgradeableProxy',[implementation.address,proxyAdmin.address,initializer]);
 const bridge=implementation.attach(proxy.address);
 const iface=artifact(tbRoot,'contracts/frost/IFrostBridge.sol','IFrostBridge');
 const frostBridge=new ethers.Contract(bridge.address,iface.abi,signer);
 const token=await deploy(kcRoot,'contracts/test/FrostTestDependencies.sol','FrostTestToken');
 const stake=await deploy(kcRoot,'contracts/test/FrostTestDependencies.sol','FrostTestStakeSource');
 const beacon=await deploy(kcRoot,'contracts/test/FrostTestDependencies.sol','FrostTestBeacon');
 const pool=await deploy(kcRoot,'@keep-network/sortition-pools/contracts/SortitionPool.sol','SortitionPool',[token.address,1]);
 await (await pool.deactivateChaosnet()).wait();
 const admission=await deploy(kcRoot,'contracts/frost/FrostAdmission.sol','FrostAdmission',[stake.address]);
 const validator=await deploy(kcRoot,'contracts/frost/FrostDkgValidator.sol','FrostDkgValidator',[3,2,2,0,0]);
 const registry=await deploy(kcRoot,'contracts/frost/FrostWalletRegistry.sol','FrostWalletRegistry',[pool.address,validator.address,admission.address,beacon.address,bridge.address,[10000,1000,10,1000],3]);
 await (await pool.transferOwnership(registry.address)).wait();
 await (await frostBridge.configureFrostRegistry(registry.address)).wait();
 // Both independent creation gates start disabled.
 if(await registry.requestsEnabled())throw Error('registry enabled by default');
 let refused=false;try{await frostBridge.callStatic.requestNewFrostWallet()}catch{refused=true}if(!refused)throw Error('Bridge enabled by default');
 const operators=[];
 for(let i=1;i<=3;i++){
  const w=new ethers.Wallet(ethers.utils.hexZeroPad(ethers.utils.hexlify(i),32),provider);operators.push(w.address);
  await provider.send('anvil_setBalance',[w.address,'0x3635c9adc5dea00000']);
  await(await stake.set(w.address,100)).wait();await(await admission.setAllowed(w.address,true)).wait();await(await registry.refreshOperator(w.address)).wait();
 }
 await(await registry.setRequestsEnabled(true)).wait();await(await frostBridge.setFrostRequestsEnabled(true)).wait();
 await(await frostBridge.requestNewFrostWallet()).wait();
 const pattern=(patternText||'1,2,3').split(',').map(Number);
 let seed=1;for(;seed<10000;seed++) {const members=await pool.selectGroup(3,ethers.utils.hexZeroPad(ethers.utils.hexlify(seed),32));if(members.every((v,i)=>Number(v)===pattern[i]))break;}if(seed===10000)throw Error('selection seed not found');
 await(await beacon.fulfill(seed)).wait();
 const epoch=Number(await registry.epoch());
 fs.writeFileSync(output,JSON.stringify({registry:registry.address,bridge:bridge.address,pool:pool.address,validator:validator.address,beacon:beacon.address,epoch,seed,operators,pattern,artifacts},null,2));
}
main().catch(e=>{console.error(e.message);process.exitCode=1});
