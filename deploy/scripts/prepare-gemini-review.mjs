// Run only after actual owner review. No Google/billing calls or automatic attestations.
import { readFileSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { resolve, relative, isAbsolute } from "node:path";
import { fileURLToPath } from "node:url";

const args = Object.fromEntries(process.argv.slice(2).map(value => {
  const at = value.indexOf("=");
  return at < 0 ? [value.replace(/^--/, ""), true] : [value.slice(2, at), value.slice(at + 1)];
}));
const required = ["key-file", "output", "provider", "project", "model"];
if (required.some(key => typeof args[key] !== "string" || !args[key].trim()) || args["confirm-billing-disabled"] !== true || args["confirm-free-model"] !== true) {
  throw new Error("After actual owner billing/quota review, provide --key-file=PRIVATE_PATH --output=PRIVATE_PATH --provider=google --project=ID --model=MODEL --confirm-billing-disabled --confirm-free-model. No values are printed.");
}
if (args.provider !== "google" || !["gemini-3.5-flash-lite", "gemini-2.5-flash-lite"].includes(args.model)) throw new Error("Provider/model is outside the reviewed adapter allowlist.");
const root = fileURLToPath(new URL("../../", import.meta.url));
for (const key of ["key-file", "output"]) {
  const within = relative(root, resolve(args[key]));
  if (!isAbsolute(args[key]) || (!isAbsolute(within) && within !== ".." && !within.startsWith(`..${process.platform === "win32" ? "\\" : "/"}`))) throw new Error("Use absolute private paths outside this checkout.");
}
const key = readFileSync(args["key-file"], "utf8").trim();
if (!/^[A-Za-z0-9_-]{20,200}$/.test(key)) throw new Error("Expected a private raw key file, not dotenv content or a command-line key.");
const review = { provider: args.provider, projectId: args.project, model: args.model, keySha256: createHash("sha256").update(key).digest("hex"), billingDisabled: true, freeTierAvailable: true, reviewedAt: new Date().toISOString() };
const raw = JSON.stringify(review);
if (Buffer.byteLength(raw) > 4096) throw new Error("Review exceeds the server configuration bound.");
writeFileSync(args.output, raw + "\n", { flag: "wx", mode: 0o600 });
process.stdout.write("Private configuration-bound review created without an expiry. Provider remains disabled. No network/billing change was made.\n");
