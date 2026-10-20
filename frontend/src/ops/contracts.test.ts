import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { decodeEvidence, decodeInvestigation, decodeTrace, decodeWorklist, ContractError } from "./contracts";
import { createFixtureOpsClient } from "./fixtures";
import { formatPaiseDecimal } from "../design/OpsPrimitives";

function fixture(name: string): unknown {
  const path = new URL(`../../../docs/api/contracts/fixtures/${name}`, import.meta.url);
  return JSON.parse(readFileSync(path, "utf8")).data;
}

describe("v2 operational contract", () => {
  it("decodes the pack's explicit worklist and partial evidence fixtures", async () => {
    const worklist = fixture("worklist-empty.json");
    const evidence = fixture("evidence-partial.json");
    assert.equal(decodeWorklist(worklist).projection.status, "ready");
    assert.equal(decodeEvidence(evidence).operations[0].presence, "unknown");
    const client = createFixtureOpsClient({ worklist, evidence });
    const record = await client.evidence("11111111-1111-4111-8111-111111111111", "ignored");
    assert.equal(record.payment.state, "PENDING_RECONCILIATION");
    await assert.rejects(client.evidence("wrong", "ignored"));
  });
  it("preserves integer money beyond JavaScript's safe number range", () => {
    assert.equal(formatPaiseDecimal("9007199254740993"), "₹90,071,992,547,409.93");
  });
  it("decodes an evidence-only investigation and an uncollected trace", () => {
    const investigation = decodeInvestigation(fixture("investigation-evidence-only.json"));
    assert.equal(investigation.report?.facts[0].assertions[0].field, "central_state");
    assert.equal(investigation.mode, "evidence_only");
    const trace = decodeTrace(fixture("trace-not-collected.json"));
    assert.equal(trace.sourceStatus, "not_collected");
  });
  it("rejects numeric money and missing projection freshness", () => {
    const worklist = fixture("worklist-empty.json") as Record<string, unknown>;
    const item = { paymentId: "id", state: "PROCESSING", amountPaise: 12500 };
    assert.throws(() => decodeWorklist({ ...worklist, items: [item] }), ContractError);
    assert.throws(() => decodeWorklist({ ...worklist, projection: {} }), ContractError);
  });
});
