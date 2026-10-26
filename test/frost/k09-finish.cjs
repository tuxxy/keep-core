// Complete the actual baseline -> current proxy upgrade, then open local DKG.
const fs=require('fs');
const path=require('path');
const inventory=require('./k09-artifacts.json')['tbtc-v2'];
const {ethers}=require(path.join(__dirname,'../../solidity/ecdsa/node_modules/ethers'));
async function main(){
 const [url,tbRoot,output,genesis]=process.argv.slice(2);
 if(!/^http:\/\/127\.0\.0\.1:\d+$/.test(url)||!/^0x[0-9a-f]{64}$/.test(genesis||''))throw Error('local chain domains required');
 const d=JSON.parse(fs.readFileSync(output));
 if(!d.baseline||d.epoch!==0)throw Error('expected public legacy bootstrap before FROST state');
 const provider=new ethers.providers.JsonRpcProvider(url),signer=provider.getSigner(0),admin=await signer.getAddress();
 function artifact(qualified){
  if(!(qualified in inventory))throw Error('missing current deployment inventory');
  const [source,name]=qualified.split(':');return JSON.parse(fs.readFileSync(path.join(tbRoot,'solidity/build',source,name+'.json')));
 }
 const a=artifact('@openzeppelin/contracts/proxy/transparent/ProxyAdmin.sol:ProxyAdmin');
 await(await new ethers.Contract(d.proxyAdmin,a.abi,signer).upgrade(d.bridge,d.implementation)).wait();
 const frost=new ethers.Contract(d.bridge,artifact('contracts/frost/IFrostBridge.sol:IFrostBridge').abi,signer);
 const kc=path.resolve(__dirname,'../../solidity/ecdsa/build');
 const registry=new ethers.Contract(d.registry,JSON.parse(fs.readFileSync(path.join(kc,'contracts/frost/FrostWalletRegistry.sol/FrostWalletRegistry.json'))).abi,signer);
 const pool=new ethers.Contract(d.pool,JSON.parse(fs.readFileSync(path.join(kc,'@keep-network/sortition-pools/contracts/SortitionPool.sol/SortitionPool.json'))).abi,signer);
 const beacon=new ethers.Contract(d.beacon,JSON.parse(fs.readFileSync(path.join(kc,'contracts/test/FrostTestDependencies.sol/FrostTestBeacon.json'))).abi,signer);
 await(await frost.configureFrostRegistry(d.registry)).wait();
 await(await frost.configureFrostFunding(d.verifier,ethers.utils.keccak256(await provider.getCode(d.verifier)),ethers.utils.id('UNQUALIFIED K09 local verifier reference'),genesis)).wait();
 const e=artifact('contracts/frost/FrostExposureController.sol:FrostExposureController');
 const exposure=await new ethers.ContractFactory(e.abi,e.bytecode,signer).deploy(d.bridge,admin);await exposure.deployed();
 if((await provider.getCode(exposure.address)).length/2-1>24576)throw Error('controller EIP-170');
 await(await exposure.openBudgetPeriod('0xffffffffffffffff','0xffffffffffffffff',ethers.utils.id('UNQUALIFIED K09 local budget; D10 OPEN'))).wait();
 await(await frost.installFrostExposureController(exposure.address)).wait();
 await(await frost.setFrostFundingGates(true,true)).wait();
 await(await frost.configureFrostRedemptions(genesis,2,1000,ethers.constants.AddressZero,ethers.utils.id('UNQUALIFIED K09 local D08 reference'))).wait();
 await(await frost.setFrostRedemptionsEnabled(true)).wait();
 await(await frost.setFrostDrainEnabled(true)).wait();
 const bridge=new ethers.Contract(d.bridge,artifact('contracts/bridge/Bridge.sol:Bridge').abi,signer);
 await(await bridge.setSpvMaintainerStatus(d.operators[0],true)).wait();
 await(await registry.setRequestsEnabled(true)).wait();await(await frost.setFrostRequestsEnabled(true)).wait();
 await(await frost.requestNewFrostWallet()).wait();
 let seed=1;for(;seed<10000;seed++){
  const members=await pool.selectGroup(3,ethers.utils.hexZeroPad(ethers.utils.hexlify(seed),32));
  if(members.every((v,i)=>Number(v)===d.pattern[i]))break;
 }
 if(seed===10000)throw Error('selection seed not found');
 await(await beacon.fulfill(seed)).wait();
 d.epoch=Number(await registry.epoch());d.seed=seed;d.exposure=exposure.address;
 fs.writeFileSync(output,JSON.stringify(d,null,2));
}
main().catch(error=>{console.error(error.message);process.exitCode=1});
