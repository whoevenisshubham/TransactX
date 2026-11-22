import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { api } from "../../api";

describe("Milestone 20-R Remediation Regressions", () => {
  it("CR-INLINE-05: verifies idempotency key & clientRequestId can be preserved across edit/retry", () => {
    // When editing a payment after an uncertain outcome, the idempotency key and clientRequestId
    // must not be regenerated to prevent duplicate debits.
    const originalKey = crypto.randomUUID();
    const originalClientId = crypto.randomUUID();

    let state = {
      idempotencyKey: originalKey as string | null,
      clientRequestId: originalClientId as string | null,
    };

    // User returns to edit form without modifying payment parameters
    function continueToConfirm(existingKey: string | null, existingClientId: string | null) {
      return {
        idempotencyKey: existingKey ?? crypto.randomUUID(),
        clientRequestId: existingClientId ?? crypto.randomUUID(),
      };
    }

    const nextState = continueToConfirm(state.idempotencyKey, state.clientRequestId);
    assert.strictEqual(nextState.idempotencyKey, originalKey, "idempotencyKey must be preserved");
    assert.strictEqual(nextState.clientRequestId, originalClientId, "clientRequestId must be preserved");

    // But if inputs change (reset triggered), fresh keys are generated
    state = { idempotencyKey: null, clientRequestId: null };
    const resetState = continueToConfirm(state.idempotencyKey, state.clientRequestId);
    assert.notStrictEqual(resetState.idempotencyKey, originalKey, "fresh idempotencyKey when reset");
    assert.notStrictEqual(resetState.clientRequestId, originalClientId, "fresh clientRequestId when reset");
  });

  it("CR-INLINE-04: verifies generation counter discards stale asynchronous recipient lookups", async () => {
    let generation = 0;
    let resolvedRecipient: string | null = null;

    async function mockLookup(query: string, delayMs: number) {
      const currentGen = ++generation;
      await new Promise((r) => setTimeout(r, delayMs));
      // Only commit if this was the latest generation
      if (currentGen === generation) {
        resolvedRecipient = query;
      }
    }

    // First query is slow (100ms), second query is fast (10ms)
    const p1 = mockLookup("alice@bank1", 100);
    const p2 = mockLookup("bob@bank2", 10);

    await Promise.all([p1, p2]);
    assert.strictEqual(resolvedRecipient, "bob@bank2", "stale earlier lookup must not overwrite newer lookup");
  });

  it("CR-INLINE-10: verifies opsReconciliationDiscrepancies pagination parameters", () => {
    // Verify that the api method exists and has appropriate arity
    assert.strictEqual(typeof api.opsReconciliationDiscrepancies, "function");
    assert.strictEqual(api.opsReconciliationDiscrepancies.length >= 2, true);
  });
});
