// Local-only K-03 deployment. Requires compiled, pinned KC and TB worktrees.
// Anvil keeps EIP-170 enabled; no stubs or unlimited-size switch are used.
const fs = require('fs');
const path = require('path');
const inventory = require('./k09-artifacts.json');
const { ethers } = require(path.join(__dirname, '../../solidity/ecdsa/node_modules/ethers'));
async function main() {
 const [url, tbRoot, output, patternText, mode, genesis] = process.argv.slice(2);
 // K-07 is K-05 plus a controlled legacy registry root, a reimbursement pool
 // and the open drain gate, for the ECDSA-to-FROST drain.
 const publicLegacy = mode === 'k09-public';
 const k07 = mode === 'k07' || publicLegacy;
 const k05 = mode === 'k05' || k07;
 if (k05 && !/^0x[0-9a-f]{64}$/.test(genesis||'')) throw Error('K-05 needs the regtest genesis');
 if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(url)) throw Error('local RPC required');
 const kcRoot = path.resolve(__dirname, '../..');
 const provider = new ethers.providers.JsonRpcProvider(url);
 const signer = provider.getSigner(0);
 const admin = await signer.getAddress();
 const libraries = new Map();
 const artifacts = {};
 const allowed = new Map([[kcRoot,new Set(Object.keys(inventory['keep-core']))],[tbRoot,new Set(Object.keys(inventory['tbtc-v2']))]]);
 function artifact(root, source, name) {
  if(!allowed.get(root)?.has(source+':'+name)) throw Error('artifact absent from deployment inventory: '+source+':'+name);
  return JSON.parse(fs.readFileSync(path.join(root, 'solidity', root===kcRoot?'ecdsa/build':'build', source, name+'.json'))); }
 async function deploy(root, source, name, args=[]) {
  const a = artifact(root,source,name);let bytecode=a.bytecode;
  for(const [file,names] of Object.entries(a.linkReferences||{})) {
   for(const [lib,positions] of Object.entries(names)) {
    allowed.get(root).add(file+':'+lib);
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
 const baseline=publicLegacy?await require('./k09-baseline.cjs').deployBaseline(tbRoot,signer):null;
 const initialImplementation=baseline?baseline.implementation:implementation;
 const proxyAdmin=await deploy(tbRoot,'@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol','ProxyAdmin');
 // K-05 adds a real Bank, the zero-difficulty regtest relay test contract and the
 // prototype verifier; the treasury is a separate account so fees are observable.
 let bank, relay, verifier, exposure, treasury=admin;
 if(k05){
  treasury=await provider.getSigner(1).getAddress();
  bank=baseline?baseline.bank:await deploy(tbRoot,'contracts/bank/Bank.sol','Bank');
  relay=await deploy(tbRoot,'contracts/test/FrostPublicFundingRegistryFixture.sol','FrostPublicFundingRegtestRelay');
  verifier=await deploy(tbRoot,'contracts/frost/FrostVerifier.sol','FrostVerifier');
 }
 let legacyRegistry, reimbursementPool;
 if(k07){
  legacyRegistry=await deploy(tbRoot,'contracts/test/FrostDrainRegistryFixture.sol','FrostDrainRegistryFixture');
  reimbursementPool=await deploy(tbRoot,'@keep-network/random-beacon/contracts/ReimbursementPool.sol','ReimbursementPool',[0,0]);
 }
 const initializer=k05?initialImplementation.interface.encodeFunctionData('initialize',[bank.address,relay.address,treasury,k07?legacyRegistry.address:admin,k07?reimbursementPool.address:admin,1]):implementation.interface.encodeFunctionData('initialize',[admin,admin,admin,admin,admin,1]);
 const proxy=await deploy(tbRoot,'@openzeppelin/contracts/proxy/transparent/TransparentUpgradeableProxy.sol','TransparentUpgradeableProxy',[initialImplementation.address,proxyAdmin.address,initializer]);
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
 if(!publicLegacy)await (await frostBridge.configureFrostRegistry(registry.address)).wait();
 // Both independent creation gates start disabled.
 if(await registry.requestsEnabled())throw Error('registry enabled by default');
 if(!publicLegacy){let refused=false;try{await frostBridge.callStatic.requestNewFrostWallet()}catch{refused=true}if(!refused)throw Error('Bridge enabled by default');}
 const operators=[];
 for(let i=1;i<=3;i++){
  const w=new ethers.Wallet(ethers.utils.hexZeroPad(ethers.utils.hexlify(i),32),provider);operators.push(w.address);
  await provider.send('anvil_setBalance',[w.address,'0x3635c9adc5dea00000']);
  await(await stake.set(w.address,100)).wait();await(await admission.setAllowed(w.address,true)).wait();await(await registry.refreshOperator(w.address)).wait();
 }
 if(k05){
  await(await bank.updateBridge(bridge.address)).wait();
  await(await bridge.setSpvMaintainerStatus(admin,true)).wait();
  await(await bridge.updateDepositParameters(3000,100,2000,30)).wait();
  const movement=Array.from(await bridge.movingFundsParameters());movement[1]=500;
  await(await bridge.updateMovingFundsParameters(...movement)).wait();
  // Local test values (D-09 OPEN): 2,000 sat caps and a 3,600 s timeout.
  await(await bridge.updateRedemptionParameters(3000,100,2000,2000,3600,0,0)).wait();
  if(!publicLegacy){
  await(await frostBridge.configureFrostFunding(verifier.address,ethers.utils.keccak256(await provider.getCode(verifier.address)),ethers.utils.id('UNQUALIFIED K-05 local verifier reference'),genesis)).wait();
  // C-10: the exposure controller, before any gate opens. The ceilings are
  // test values (D-10 OPEN): uint64 max, so no earlier K-05..K-08 path is
  // refused by the budget; the C-10 tests lower them by a new period.
  exposure=await deploy(tbRoot,'contracts/frost/FrostExposureController.sol','FrostExposureController',[bridge.address,admin]);
  await(await exposure.openBudgetPeriod('0xffffffffffffffff','0xffffffffffffffff',ethers.utils.id('UNQUALIFIED C-10 local test budget; D-10 OPEN'))).wait();
  await(await frostBridge.installFrostExposureController(exposure.address)).wait();
  await(await frostBridge.setFrostFundingGates(true,true)).wait();
  await(await frostBridge.configureFrostRedemptions(genesis,2,1000,ethers.constants.AddressZero,ethers.utils.id('UNQUALIFIED K-05 local D-08 reference'))).wait();
  await(await frostBridge.setFrostRedemptionsEnabled(true)).wait();
  }
 }
 let legacy;
 if(k07){
  await(await legacyRegistry.bindWalletOwner(bridge.address)).wait();
  await(await legacyRegistry.setMember(admin)).wait();
  await(await reimbursementPool.authorize(bridge.address)).wait();
  if(!publicLegacy)await(await frostBridge.setFrostDrainEnabled(true)).wait();
  legacy=legacyLayout(tbRoot);
  legacy.registry=legacyRegistry.address;
  // The real legacy proposal validator, for the legacy host's drain proposal.
  legacy.validator=(await deploy(tbRoot,'contracts/bridge/WalletProposalValidator.sol','WalletProposalValidator',[bridge.address])).address;
 }
 const pattern=(patternText||'1,2,3').split(',').map(Number);
 let epoch=0, seed=0;
 if(!publicLegacy){
 await(await registry.setRequestsEnabled(true)).wait();await(await frostBridge.setFrostRequestsEnabled(true)).wait();
 await(await frostBridge.requestNewFrostWallet()).wait();
 seed=1;for(;seed<10000;seed++) {const members=await pool.selectGroup(3,ethers.utils.hexZeroPad(ethers.utils.hexlify(seed),32));if(members.every((v,i)=>Number(v)===pattern[i]))break;}if(seed===10000)throw Error('selection seed not found');
 await(await beacon.fulfill(seed)).wait();
 epoch=Number(await registry.epoch());
 }
 fs.writeFileSync(output,JSON.stringify({proxyAdmin:proxyAdmin.address,implementation:implementation.address,verifier:verifier?.address,baseline:baseline?.manifest,legacy,bank:bank?bank.address:undefined,exposure:exposure?exposure.address:undefined,treasury,registry:registry.address,bridge:bridge.address,pool:pool.address,validator:validator.address,beacon:beacon.address,epoch,seed,operators,pattern,artifacts},null,2));
}
// legacyLayout reads the compiler's Bridge storage layout, so the K-07 tests
// can set the controlled legacy wallet markers (main UTXO hash, state) without
// hard-coding slots.
function legacyLayout(tbRoot) {
 // The artifact's debug file names the exact build that produced it.
 const dbg=path.join(tbRoot,'solidity','build','contracts','bridge','Bridge.sol','Bridge.dbg.json');
 for(const f of [path.resolve(path.dirname(dbg),JSON.parse(fs.readFileSync(dbg)).buildInfo)]) {
  const info=JSON.parse(fs.readFileSync(f));
  const layout=info.output?.contracts?.['contracts/bridge/Bridge.sol']?.Bridge?.storageLayout;
  if(!layout)continue;
  const self=layout.storage.find(s=>s.label==='self');
  const members=layout.types[self.type].members;
  const field=(name)=>{const m=members.find(x=>x.label===name);if(!m)throw Error('missing '+name);return m;};
  const wallets=field('registeredWallets');
  const wallet=layout.types[layout.types[wallets.type].value].members;
  const member=(name)=>{const m=wallet.find(x=>x.label===name);return {slot:Number(m.slot),offset:m.offset,bytes:Number(layout.types[m.type].numberOfBytes)};};
  const live=field('liveWalletsCount');
  return {registeredWallets:Number(self.slot)+Number(wallets.slot),mainUtxoHash:member('mainUtxoHash'),state:member('state'),movingFundsRequestedAt:member('movingFundsRequestedAt'),liveWalletsCount:{slot:Number(self.slot)+Number(live.slot),offset:live.offset,bytes:Number(layout.types[live.type].numberOfBytes)}};
 }
 throw Error('no Bridge storage layout in build-info');
}
main().catch(e=>{console.error(e.message);process.exitCode=1});
