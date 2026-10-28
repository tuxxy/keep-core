// Independent K-04/C-04 intent encoder. Run with Node 22 --experimental-strip-types.
// Uses only fixed field widths, explicit endian choices and byte strings.
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
const file = fileURLToPath(new URL("../../pkg/frost/signing/testdata/intent-v2.json", import.meta.url));
const vectors = JSON.parse(readFileSync(file, "utf8"));
const hash = (b: Buffer) => createHash("sha256").update(b).digest();
function hex(s: string, width?: number): Buffer {
  if (!/^(?:[0-9a-f]{2})*$/.test(s)) throw Error("invalid hex");
  const b=Buffer.from(s,"hex"); if(width!==undefined && b.length!==width) throw Error("wrong width"); return b;
}
function uint(n: string|number, width: number, little=false): Buffer {
  let v=BigInt(n); if(v<0n || v >= (1n<<BigInt(8*width))) throw Error("integer overflow");
  const out=Buffer.alloc(width);for(let i=0;i<width;i++){out[little?i:width-1-i]=Number(v&255n);v>>=8n;}return out;
}
function compact(n: number): Buffer {
  return n<253 ? uint(n,1) : n<=65535 ? Buffer.concat([Buffer.from([253]),uint(n,2,true)]) : Buffer.concat([Buffer.from([254]),uint(n,4,true)]);
}
let checked=0;
for(const v of vectors){
 const prev=v.previous.flatMap((p:{value:string,script:string})=>{const s=hex(p.script);return [uint(p.value,8,true),compact(s.length),s]});
 const snapshot=hash(Buffer.concat([Buffer.from("keep-core/taproot-snapshot/v1"),hex(v.outputKey,32),hex(v.rawTransaction),...prev]));
 if(snapshot.toString("hex")!==v.snapshot)throw Error("snapshot mismatch: "+v.name);
 const requests=v.requests.flatMap((r:{id:string,value:string,script:string})=>{const s=hex(r.script);return [hex(r.id,32),uint(r.value,8),uint(s.length,2),s]});
 const preimage=Buffer.concat([
  Buffer.from("keep-core/frost/signing-intent/v2/"),uint(2,1),snapshot,
  hex(v.walletID,32),hex(v.descriptorHash,32),hex(v.genesis,32),uint(1,1),
  uint(v.generation,8),uint(v.stateVersion,8),uint(v.requests.length,2),...requests,
  hex(v.conflictHash,32),uint(v.conflictIndex,4),...v.fees.map((x:string)=>uint(x,8))
 ]);
 if(preimage.toString("hex")!==v.preimage || hash(preimage).toString("hex")!==v.commitment)throw Error("intent mismatch: "+v.name);
 console.log(v.name+": "+v.commitment);checked++;
}
if(checked!==2)throw Error("expected two fixed vectors");
