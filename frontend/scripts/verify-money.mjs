import assert from "node:assert/strict";

/** Mirrors frontend/src/money.ts parsePaise for CI without a browser test runner. */
function parsePaise(input) {
  const value = input.trim();
  if (!/^(0|[1-9]\d*)(?:\.(\d{1,2}))?$/.test(value)) return null;
  const [whole, fraction = ""] = value.split(".");
  const paise = BigInt(whole) * 100n + BigInt((fraction + "00").slice(0, 2));
  if (paise <= 0n || paise > BigInt(Number.MAX_SAFE_INTEGER)) return null;
  return Number(paise);
}

const rejected = ["", "0", "-10", "1..5", "1.999", "1.2345", "abc", "01", ".5", "10."];
for (const value of rejected) {
  assert.equal(parsePaise(value), null, `expected reject ${JSON.stringify(value)}`);
}

assert.equal(parsePaise("125"), 12500);
assert.equal(parsePaise("125.5"), 12550);
assert.equal(parsePaise("125.50"), 12550);
assert.equal(parsePaise("0.01"), 1);
assert.equal(parsePaise("0.1"), 10);

console.log("money parsePaise checks passed");
