import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomBytes, createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

test("review generation needs explicit owner confirmations and binds configuration without expiry", () => {
  const folder = mkdtempSync(join(tmpdir(), "transactx-review-"));
  const key = randomBytes(24).toString("hex"); // Synthetic local test credential only.
  const keyPath = join(folder, "key.txt");
  const output = join(folder, "review.json");
  writeFileSync(keyPath, key, {mode:0o600});
  const script = fileURLToPath(new URL("./prepare-gemini-review.mjs", import.meta.url));
  const args = [script, `--key-file=${keyPath}`, `--output=${output}`, "--provider=google", "--project=fixture-project", "--model=gemini-3.5-flash-lite"];
  const run = extra => execFileSync(process.execPath, [...args, ...extra], {encoding:"utf8",stdio:"pipe"});
  assert.throws(() => run([]));
  assert.throws(() => run(["--confirm-billing-disabled"]));
  assert.equal(existsSync(output), false);
  const message = run(["--confirm-billing-disabled", "--confirm-free-model"]);
  assert.equal(message.includes(key), false);
  const raw = readFileSync(output,"utf8");
  assert.equal(raw.includes(key),false);
  const review = JSON.parse(raw);
  assert.equal(review.provider,"google"); assert.equal(review.projectId,"fixture-project");
  assert.equal(review.model,"gemini-3.5-flash-lite");
  assert.equal(review.keySha256,createHash("sha256").update(key).digest("hex"));
  assert.ok(review.billingDisabled && review.freeTierAvailable && review.reviewedAt);
  assert.equal("validUntil" in review,false);
  assert.throws(() => run(["--confirm-billing-disabled", "--confirm-free-model"]));
  assert.equal(readFileSync(output,"utf8"),raw); // Never overwrite an earlier review.
});

test("opaque dotted keys remain bound while dotenv and header injection are rejected", () => {
  const folder = mkdtempSync(join(tmpdir(), "transactx-review-format-"));
  const script = fileURLToPath(new URL("./prepare-gemini-review.mjs", import.meta.url));
  for (const [index,key] of ["opaque."+randomBytes(24).toString("hex"), "GEMINI_API_KEY="+randomBytes(24).toString("hex"), "opaque"+randomBytes(24).toString("hex")+"\r\nx-header: injected"].entries()) {
    const keyPath=join(folder,`key-${index}.txt`), output=join(folder,`review-${index}.json`);
    writeFileSync(keyPath,key,{mode:0o600});
    const run=()=>execFileSync(process.execPath,[script,`--key-file=${keyPath}`,`--output=${output}`,"--provider=google","--project=fixture-project","--model=gemini-2.5-flash-lite","--confirm-billing-disabled","--confirm-free-model"],{encoding:"utf8",stdio:"pipe"});
    if(index===0){assert.equal(run().includes(key),false);assert.equal(JSON.parse(readFileSync(output,"utf8")).keySha256,createHash("sha256").update(key).digest("hex"));}
    else {assert.throws(run);assert.equal(existsSync(output),false);}
  }
});
