// Local owner attestation after reviewing the actual project's free quota/billing.
// This command never contacts Google, changes billing or enables the provider.
import { readFileSync,writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { resolve,relative,isAbsolute } from "node:path";
import { fileURLToPath } from "node:url";
const args=Object.fromEntries(process.argv.slice(2).map(value=>{const at=value.indexOf("=");return at<0?[value.replace(/^--/,""),true]:[value.slice(2,at),value.slice(at+1)];}));
const required=["key-file","output","project","model"];
if(required.some(key=>typeof args[key]!=="string") || args["confirm-billing-disabled"]!==true || args["confirm-free-model"]!==true) throw new Error("After actual owner review, provide --key-file=PRIVATE_PATH --output=PRIVATE_PATH --project=ID --model=MODEL --confirm-billing-disabled --confirm-free-model. No values are printed.");
if(!["gemini-3.5-flash-lite","gemini-2.5-flash-lite"].includes(args.model))throw new Error("Model is outside the reviewed adapter allowlist.");
const root=fileURLToPath(new URL("../../",import.meta.url));
for(const key of ["key-file","output"]){if(!isAbsolute(args[key])||!relative(root,resolve(args[key])).startsWith(".."))throw new Error("Key and approval must use absolute private paths outside this checkout.");}
const key=readFileSync(args["key-file"],"utf8").trim();
if(!/^[A-Za-z0-9_-]{20,200}$/.test(key))throw new Error("Expected a private raw API-key file; do not pass dotenv content or a key on the command line.");
const reviewedAt=new Date();
const approval={projectId:args.project,model:args.model,keySha256:createHash("sha256").update(key).digest("hex"),billingDisabled:true,freeTierAvailable:true,reviewedAt:reviewedAt.toISOString(),validUntil:new Date(reviewedAt.getTime()+86400000).toISOString()};
writeFileSync(args.output,JSON.stringify(approval,null,2)+"\n",{flag:"wx",mode:0o600});
process.stdout.write("Private approval created for 24 hours. Provider remains disabled; no network request was made.\n");
