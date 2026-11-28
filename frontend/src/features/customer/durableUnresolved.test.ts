import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  ActiveReservationConflictError,
  classifyPaymentOutcome,
  computePayloadHash,
  DurableUnresolvedStore,
  isTerminalOutcome,
  isTerminalState,
  StorageUnavailableError,
  type DurableUnresolvedIntent,
} from "../../unresolvedStore";
import { api, ApiError } from "../../api";

describe("Milestone 20-R.2 Durable Unresolved Payment Lifecycle & Recovery", () => {
  function makeMockIntent(
    ownerUserId: string,
    overrides?: Partial<DurableUnresolvedIntent>
  ): DurableUnresolvedIntent {
    const now = new Date().toISOString();
    return {
      ownerUserId,
      clientRequestId: crypto.randomUUID(),
      idempotencyKey: crypto.randomUUID(),
      recipient: {
        name: "Test Merchant",
        paymentIdentifier: "merchant@transactx",
        status: "ACTIVE",
      },
      amountPaise: 150000,
      amount: "1500.00",
      currency: "INR",
      note: "Invoice #1042",
      payloadHash: "sha256-mock-hash",
      createdAt: now,
      updatedAt: now,
      state: "UNCERTAIN",
      ...overrides,
    };
  }

  it("P0-1: Durable store saves and retrieves UNCERTAIN intent in memory fallback", async () => {
    // When indexedDB is unavailable (Node.js test environment without IDB), memory mode activates
    const store = new DurableUnresolvedStore("user-alice-123", "test-db", undefined);
    const intent = makeMockIntent("user-alice-123");

    const saved = await store.save(intent);
    assert.strictEqual(saved.clientRequestId, intent.clientRequestId);
    assert.strictEqual(saved.persistenceFailed, true, "Should flag persistenceFailed in memory fallback mode");

    const active = await store.getActive();
    assert.notStrictEqual(active, null);
    assert.strictEqual(active?.clientRequestId, intent.clientRequestId);
    assert.strictEqual(active?.idempotencyKey, intent.idempotencyKey);
    assert.strictEqual(active?.state, "UNCERTAIN");

    store.close();
  });

  it("P0-2: Tenant isolation strictly prevents cross-user access to unresolved intents", async () => {
    const storeAlice = new DurableUnresolvedStore("user-alice", "test-db-isolation", undefined);
    const storeBob = new DurableUnresolvedStore("user-bob", "test-db-isolation", undefined);

    const intentAlice = makeMockIntent("user-alice");
    await storeAlice.save(intentAlice);

    // Bob tries to query active intent
    const bobActive = await storeBob.getActive();
    assert.strictEqual(bobActive, null, "User Bob must not see User Alice's unresolved intent");

    // Bob tries to get by clientRequestId
    const bobQuery = await storeBob.get(intentAlice.clientRequestId);
    assert.strictEqual(bobQuery, null, "User Bob must not retrieve Alice's intent by clientRequestId");

    // Saving an intent with wrong owner must throw tenant violation error
    await assert.rejects(
      async () => {
        await storeBob.save(intentAlice);
      },
      /Tenant isolation violation/,
      "Must reject saving an intent belonging to a different user"
    );

    storeAlice.close();
    storeBob.close();
  });

  it("P0-3: ClearResolved refuses to delete an UNCERTAIN intent", async () => {
    const store = new DurableUnresolvedStore("user-alice", "test-db-safe-clear", undefined);
    const intent = makeMockIntent("user-alice", { state: "UNCERTAIN" });
    await store.save(intent);

    // Attempting to clear an active UNCERTAIN intent must be a no-op
    await store.clearResolved(intent.clientRequestId);

    const stillThere = await store.get(intent.clientRequestId);
    assert.notStrictEqual(stillThere, null, "UNCERTAIN intent must NOT be deleted by clearResolved");
    assert.strictEqual(stillThere?.state, "UNCERTAIN");

    // Mark resolved, then clearResolved should succeed
    await store.markResolved(intent.clientRequestId, "RESOLVED_COMPLETED", "pmt-resolved-99");
    const marked = await store.get(intent.clientRequestId);
    assert.strictEqual(marked?.state, "RESOLVED_COMPLETED");
    assert.strictEqual(marked?.paymentId, "pmt-resolved-99");

    await store.clearResolved(intent.clientRequestId);
    const deleted = await store.get(intent.clientRequestId);
    assert.strictEqual(deleted, null, "RESOLVED intent may be safely cleared");

    store.close();
  });

  it("P0-4: Status check 404 preserves unresolved intent and never discards cryptographic identity", async () => {
    const originalFetch = globalThis.fetch;
    const store = new DurableUnresolvedStore("user-alice", "test-db-status404", undefined);
    const intent = makeMockIntent("user-alice");
    await store.save(intent);

    // Mock API returning 404 PAYMENT_NOT_FOUND (in-flight backend processing delay)
    globalThis.fetch = async () => {
      return new Response(
        JSON.stringify({ error: { code: "PAYMENT_NOT_FOUND", message: "transaction not found" } }),
        { status: 404, headers: { "Content-Type": "application/json" } }
      );
    };

    try {
      let threw404 = false;
      try {
        await api.paymentByIdempotencyKey(intent.idempotencyKey, "mock-token");
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) {
          threw404 = true;
          // As required by M20-R.2: 404 must NOT clear or discard the intent!
          // It records check timestamp and maintains UNCERTAIN state
          const now = new Date().toISOString();
          await store.save({
            ...intent,
            updatedAt: now,
            lastStatusCheckAt: now,
            lastHttpStatus: 404,
          });
        }
      }

      assert.strictEqual(threw404, true, "Must have received 404 status from in-flight lookup");

      // Verify intent remains active in store with unchanged idempotencyKey
      const active = await store.getActive();
      assert.notStrictEqual(active, null);
      assert.strictEqual(active?.idempotencyKey, intent.idempotencyKey);
      assert.strictEqual(active?.clientRequestId, intent.clientRequestId);
      assert.strictEqual(active?.state, "UNCERTAIN");
      assert.strictEqual(active?.lastHttpStatus, 404);
    } finally {
      globalThis.fetch = originalFetch;
      store.close();
    }
  });

  it("P0-5: Status check 200 resolves intent with payment ID", async () => {
    const originalFetch = globalThis.fetch;
    const store = new DurableUnresolvedStore("user-alice", "test-db-status200", undefined);
    const intent = makeMockIntent("user-alice");
    await store.save(intent);

    const mockCompletedPayment = {
      id: "pmt-authoritative-777",
      amountPaise: 150000,
      currency: "INR",
      state: "COMPLETED",
      origin: "ONLINE",
      createdAt: new Date().toISOString(),
      senderName: "Alice",
      senderPaymentIdentifier: "alice@transactx",
      receiverName: "Test Merchant",
      receiverPaymentIdentifier: "merchant@transactx",
      direction: "SENT",
    };

    globalThis.fetch = async () => {
      return new Response(JSON.stringify({ data: mockCompletedPayment }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    };

    try {
      const resolved = await api.paymentByIdempotencyKey(intent.idempotencyKey, "mock-token");
      assert.strictEqual(resolved.id, "pmt-authoritative-777");
      assert.strictEqual(resolved.state, "COMPLETED");

      // Authoritative resolution transitions state
      await store.markResolved(intent.clientRequestId, "RESOLVED_COMPLETED", resolved.id);

      const activeAfter = await store.getActive();
      assert.strictEqual(activeAfter, null, "Active unresolved intent must be cleared upon authoritative completion");

      const resolvedRecord = await store.get(intent.clientRequestId);
      assert.strictEqual(resolvedRecord?.state, "RESOLVED_COMPLETED");
      assert.strictEqual(resolvedRecord?.paymentId, "pmt-authoritative-777");
    } finally {
      globalThis.fetch = originalFetch;
      store.close();
    }
  });

  it("P0-6: Lifecycle unmount simulation preserves locked intent across navigation", async () => {
    // Simulate user navigating: SendMoneyPage mounts -> fails with 500 -> unmounts -> remounts
    const store = new DurableUnresolvedStore("user-alice", "test-db-nav", undefined);
    const initialIntent = makeMockIntent("user-alice", {
      amountPaise: 500000,
      note: "Cross-navigation verification",
    });

    // 1. SendMoneyPage saves uncertain intent on 500
    await store.save(initialIntent);

    // 2. Component unmounts (e.g. user clicks Activity or Overview).
    // SendMoneyPage state is destroyed. But store holds the record.
    // 3. User navigates back to Send money. SendMoneyPage initializes from store.getActive():
    const recovered = await store.getActive();
    assert.notStrictEqual(recovered, null);
    assert.strictEqual(recovered?.clientRequestId, initialIntent.clientRequestId);
    assert.strictEqual(recovered?.idempotencyKey, initialIntent.idempotencyKey);
    assert.strictEqual(recovered?.amountPaise, 500000);
    assert.strictEqual(recovered?.note, "Cross-navigation verification");

    store.close();
  });
});

describe("Milestone 20-R.3 Pre-Dispatch Reservation, Terminal States & Cross-Tab Safety", () => {
  function makeMockIntent(
    ownerUserId: string,
    overrides?: Partial<DurableUnresolvedIntent>
  ): DurableUnresolvedIntent {
    const now = new Date().toISOString();
    return {
      ownerUserId,
      clientRequestId: crypto.randomUUID(),
      idempotencyKey: crypto.randomUUID(),
      recipient: {
        name: "Test Merchant",
        paymentIdentifier: "merchant@transactx",
        status: "ACTIVE",
      },
      amountPaise: 150000,
      amount: "1500.00",
      currency: "INR",
      note: "Invoice #1042",
      payloadHash: "sha256-mock-hash",
      createdAt: now,
      updatedAt: now,
      state: "DISPATCHING",
      ...overrides,
    };
  }

  it("P0-7: Pre-dispatch reservation persists active reservation before dispatch", async () => {
    // When memory fallback is permitted in test harness
    const store = new DurableUnresolvedStore("user-alice-777", "test-db-p07", undefined, true);
    const intent = makeMockIntent("user-alice-777", { state: "DISPATCHING" });

    await store.reserveActiveIntent(intent);

    const activeRes = await store.getActiveReservation();
    assert.notStrictEqual(activeRes, null);
    assert.strictEqual(activeRes?.ownerUserId, "user-alice-777");
    assert.strictEqual(activeRes?.clientRequestId, intent.clientRequestId);
    assert.strictEqual(activeRes?.state, "DISPATCHING");

    const fullActive = await store.getActive();
    assert.notStrictEqual(fullActive, null);
    assert.strictEqual(fullActive?.clientRequestId, intent.clientRequestId);
    assert.strictEqual(fullActive?.state, "DISPATCHING");

    store.close();
  });

  it("P0-8: Terminal resolution clears active reservation but retains resolved record", async () => {
    const store = new DurableUnresolvedStore("user-alice-888", "test-db-p08", undefined, true);
    const intent = makeMockIntent("user-alice-888", { state: "DISPATCHING" });

    await store.reserveActiveIntent(intent);
    assert.notStrictEqual(await store.getActiveReservation(), null);

    // Resolve terminal COMPLETED
    await store.resolveTerminal(intent.clientRequestId, "RESOLVED_COMPLETED", "pmt-888-comp");

    // Active reservation MUST be cleared
    const activeResAfter = await store.getActiveReservation();
    assert.strictEqual(activeResAfter, null, "Active reservation must be deleted upon terminal resolution");

    const activeIntentAfter = await store.getActive();
    assert.strictEqual(activeIntentAfter, null, "getActive() must return null after terminal resolution");

    // Full intent record remains queryable by clientRequestId
    const record = await store.get(intent.clientRequestId);
    assert.notStrictEqual(record, null);
    assert.strictEqual(record?.state, "RESOLVED_COMPLETED");
    assert.strictEqual(record?.paymentId, "pmt-888-comp");

    store.close();
  });

  it("P0-9: Cross-tab active reservation conflict blocks duplicate payment intent", async () => {
    const store = new DurableUnresolvedStore("user-alice-999", "test-db-p09", undefined, true);
    const intent1 = makeMockIntent("user-alice-999", { state: "DISPATCHING" });
    const intent2 = makeMockIntent("user-alice-999", { state: "DISPATCHING" });

    await store.reserveActiveIntent(intent1);

    // Attempting to reserve a second concurrent intent under the same owner must throw ActiveReservationConflictError
    await assert.rejects(
      async () => {
        await store.reserveActiveIntent(intent2);
      },
      (err: unknown) => {
        return (
          err instanceof ActiveReservationConflictError &&
          err.existingReservation.clientRequestId === intent1.clientRequestId
        );
      },
      "Must throw ActiveReservationConflictError referencing the existing reservation"
    );

    // Re-reserving the SAME clientRequestId is allowed (idempotent progression)
    const intent1Updated: DurableUnresolvedIntent = {
      ...intent1,
      state: "PROCESSING",
      updatedAt: new Date().toISOString(),
    };
    await store.reserveActiveIntent(intent1Updated);

    const active = await store.getActiveReservation();
    assert.strictEqual(active?.clientRequestId, intent1.clientRequestId);
    assert.strictEqual(active?.state, "PROCESSING");

    store.close();
  });

  it("P0-10: Fail-closed storage throws StorageUnavailableError when storage disabled", async () => {
    // When allowMemoryFallback = false (production default) and indexedDB is unavailable
    const store = new DurableUnresolvedStore("user-alice-failclosed", "test-db-p10", undefined, false);
    const intent = makeMockIntent("user-alice-failclosed");

    await assert.rejects(
      async () => {
        await store.reserveActiveIntent(intent);
      },
      (err: unknown) => {
        return err instanceof StorageUnavailableError;
      },
      "Must fail closed and throw StorageUnavailableError when durable storage is unavailable"
    );

    store.close();
  });

  it("P0-11: Outcome classifier correctly partitions terminal vs intermediate vs unknown states", () => {
    // Terminal states
    assert.strictEqual(classifyPaymentOutcome("COMPLETED"), "TERMINAL_COMPLETED");
    assert.strictEqual(classifyPaymentOutcome({ state: "completed" }), "TERMINAL_COMPLETED");
    assert.strictEqual(classifyPaymentOutcome("FAILED"), "TERMINAL_FAILED");
    assert.strictEqual(classifyPaymentOutcome({ state: "FAILED" }), "TERMINAL_FAILED");
    assert.strictEqual(classifyPaymentOutcome("REVERSED"), "TERMINAL_REVERSED");
    assert.strictEqual(classifyPaymentOutcome({ state: "REVERSED" }), "TERMINAL_REVERSED");

    assert.strictEqual(isTerminalOutcome("TERMINAL_COMPLETED"), true);
    assert.strictEqual(isTerminalOutcome("TERMINAL_FAILED"), true);
    assert.strictEqual(isTerminalOutcome("TERMINAL_REVERSED"), true);
    assert.strictEqual(isTerminalState("RESOLVED_COMPLETED"), true);
    assert.strictEqual(isTerminalState("RESOLVED_FAILED"), true);
    assert.strictEqual(isTerminalState("RESOLVED_REVERSED"), true);

    // Non-terminal intermediate states
    const intermediateStates = [
      "CREATED",
      "VALIDATING",
      "LOCAL_SETTLEMENT",
      "ROUTING",
      "PROCESSING",
      "COMMITTED",
      "BANK_SETTLED_CENTRAL_PENDING",
      "PENDING_RECONCILIATION",
      "OFFLINE_CAPTURED",
      "QUEUED",
      "SYNCING",
      "REPLAY_FAILED",
    ];
    for (const st of intermediateStates) {
      assert.strictEqual(classifyPaymentOutcome(st), "NON_TERMINAL_PROCESSING");
      assert.strictEqual(classifyPaymentOutcome({ state: st }), "NON_TERMINAL_PROCESSING");
      assert.strictEqual(isTerminalOutcome(classifyPaymentOutcome(st)), false);
    }

    assert.strictEqual(isTerminalState("DISPATCHING"), false);
    assert.strictEqual(isTerminalState("PROCESSING"), false);
    assert.strictEqual(isTerminalState("UNCERTAIN"), false);
    assert.strictEqual(isTerminalState("PREPARED"), false);

    // Unknown or invalid states fail closed
    assert.strictEqual(classifyPaymentOutcome("UNKNOWN_STATE_XYZ"), "UNKNOWN_STATE");
    assert.strictEqual(classifyPaymentOutcome(null), "UNKNOWN_STATE");
    assert.strictEqual(classifyPaymentOutcome(undefined), "UNKNOWN_STATE");
    assert.strictEqual(isTerminalOutcome("UNKNOWN_STATE"), false);
  });

  it("P0-12: Canonical payload fingerprinting produces deterministic SHA-256 hashes", async () => {
    const payloadA = {
      recipient: "bob@transactx",
      amountPaise: 10000,
      currency: "INR",
      note: "Test transfer",
    };

    // Different key ordering must produce the exact same hash
    const payloadB = {
      note: "Test transfer",
      currency: "INR",
      amountPaise: 10000,
      recipient: "bob@transactx",
    };

    const hashA = await computePayloadHash(payloadA);
    const hashB = await computePayloadHash(payloadB);

    assert.strictEqual(typeof hashA, "string");
    assert.strictEqual(hashA.length > 0, true);
    assert.strictEqual(hashA, hashB, "Hash must be key-order invariant (canonical JSON)");

    // Changed payload produces different hash
    const hashC = await computePayloadHash({ ...payloadA, amountPaise: 20000 });
    assert.notStrictEqual(hashA, hashC, "Different payload amounts must produce distinct hashes");
  });

  it("P0-13: Delayed updateProgress(UNCERTAIN) cannot overwrite a terminal RESOLVED_COMPLETED intent", async () => {
    const store = new DurableUnresolvedStore("user-alice", "test-db-terminal-guard", undefined);
    const intent = makeMockIntent("user-alice");
    await store.save(intent);
    await store.resolveTerminal(intent.clientRequestId, "RESOLVED_COMPLETED", "pmt-settled-1");

    // Simulate a late 404 or background tab status check trying to regress to UNCERTAIN
    await store.updateProgress(intent.clientRequestId, "UNCERTAIN", undefined, 404, "Late 404");

    const active = await store.getActive();
    assert.strictEqual(active, null, "Active reservation must remain cleared after terminal resolution");
    const record = await store.get(intent.clientRequestId);
    assert.strictEqual(record?.state, "RESOLVED_COMPLETED");
    store.close();
  });
});
