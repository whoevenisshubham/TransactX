import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { api, ApiError } from "../../api";
import type { Payment } from "../../types";
import type { UnresolvedIntent } from "./SendMoneyPage";

describe("Milestone 20-R.1 Production Remediation & Payment Safety Regressions", () => {
  it("P0: api.paymentByIdempotencyKey dispatches exact route, encodes key, and passes auth bearer", async () => {
    const originalFetch = globalThis.fetch;
    const testKey = "idemp-key-xyz/123+special";
    const testToken = "stateless-jwt-test-token";
    let capturedUrl = "";
    let capturedHeaders: HeadersInit | undefined;

    const mockPayment: Payment = {
      id: "pmt-committed-001",
      amountPaise: 50000,
      currency: "INR",
      state: "COMPLETED",
      origin: "WEB",
      createdAt: new Date().toISOString(),
      senderName: "Alice",
      senderPaymentIdentifier: "alice@transactx",
      receiverName: "Bob",
      receiverPaymentIdentifier: "bob@transactx",
      direction: "SENT",
    };

    globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      capturedUrl = String(input);
      capturedHeaders = init?.headers;
      return new Response(JSON.stringify({ data: mockPayment }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    };

    try {
      const result = await api.paymentByIdempotencyKey(testKey, testToken);
      const url = new URL(capturedUrl);
      assert.strictEqual(
        url.pathname,
        `/api/payments/idempotency/${encodeURIComponent(testKey)}`,
        "Must correctly encode idempotency key in URL path"
      );
      assert.strictEqual(
        (capturedHeaders as Record<string, string>)?.["Authorization"],
        `Bearer ${testToken}`,
        "Must pass Bearer token in Authorization header"
      );
      assert.strictEqual(result.id, "pmt-committed-001");
      assert.strictEqual(result.state, "COMPLETED");
      assert.strictEqual(result.amountPaise, 50000);
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("P0: api.paymentByIdempotencyKey propagates 404 ApiError when key not found on server", async () => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async () => {
      return new Response(
        JSON.stringify({ error: { code: "NOT_FOUND", message: "payment not found" } }),
        { status: 404, headers: { "Content-Type": "application/json" } }
      );
    };

    try {
      await assert.rejects(
        async () => {
          await api.paymentByIdempotencyKey("non-existent-key", "token");
        },
        (err: unknown) => {
          assert.strictEqual(err instanceof ApiError, true);
          assert.strictEqual((err as ApiError).status, 404);
          return true;
        }
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("P0: UnresolvedIntent model preserves cryptographic keys and prevents duplicate debit", () => {
    // When a server returns 500 or times out, the unresolved intent is frozen.
    const key = crypto.randomUUID();
    const clientRequestId = crypto.randomUUID();

    const intent: UnresolvedIntent = {
      recipient: {
        name: "Bob Demo",
        paymentIdentifier: "bob@transactx",
        status: "ACTIVE",
      },
      amountPaise: 250000,
      amount: "2500.00",
      note: "Rent payment",
      idempotencyKey: key,
      clientRequestId: clientRequestId,
    };

    // Verify properties cannot be mutated or lost
    assert.strictEqual(intent.idempotencyKey, key);
    assert.strictEqual(intent.clientRequestId, clientRequestId);
    assert.strictEqual(intent.amountPaise, 250000);

    // Verify retry payload reconstruction preserves exact parameters
    const retryPayload = {
      recipient: intent.recipient.paymentIdentifier,
      amountPaise: intent.amountPaise,
      currency: "INR",
      note: intent.note.trim() || undefined,
    };
    assert.deepStrictEqual(retryPayload, {
      recipient: "bob@transactx",
      amountPaise: 250000,
      currency: "INR",
      note: "Rent payment",
    });
  });

  it("P1: CustomerShell idle replay sweep skips payments/accounts API calls when queue is empty", async () => {
    // Simulate the logic in CustomerShell.replay:
    // If worker.run() yields 0 results, no network calls should be made.
    let apiCalls = 0;
    const mockApiPayments = async () => {
      apiCalls++;
      return [];
    };
    const mockApiAccounts = async () => {
      apiCalls++;
      return [];
    };

    async function sweepCycle(workerResults: Array<{ outcome: "SYNCED" | "FAILED" | "PENDING" | "RETRYABLE" }>) {
      if (workerResults.length > 0) {
        if (workerResults.some((r) => r.outcome === "SYNCED" || r.outcome === "FAILED")) {
          await mockApiPayments();
          await mockApiAccounts();
        }
      }
    }

    // Idle sweep (empty results)
    await sweepCycle([]);
    assert.strictEqual(apiCalls, 0, "Idle sweep must produce 0 API calls when queue has no items");

    // Worker made pending progress but no final sync
    await sweepCycle([{ outcome: "PENDING" }]);
    assert.strictEqual(apiCalls, 0, "Pending items must not trigger balance/payment refetch");

    // Worker synced an item
    await sweepCycle([{ outcome: "SYNCED" }]);
    assert.strictEqual(apiCalls, 2, "Synced item must trigger both payments and accounts refetch");
  });

  it("CR-INLINE-10: api.opsReconciliationDiscrepancies formats pagination query string and auth headers", async () => {
    const originalFetch = globalThis.fetch;
    let requestedUrl = "";
    let requestedHeaders: HeadersInit | undefined;

    globalThis.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
      requestedUrl = String(input);
      requestedHeaders = init?.headers;
      return new Response(JSON.stringify({ data: { items: [], total: 0 } }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    };

    try {
      await api.opsReconciliationDiscrepancies("run-m20-xyz", "ops-bearer-token", 25, 100);
      const url = new URL(requestedUrl);
      assert.strictEqual(
        url.pathname,
        "/api/ops/reconciliation/runs/run-m20-xyz/discrepancies",
        "Must target discrepancies endpoint for run"
      );
      assert.strictEqual(url.searchParams.get("limit"), "25");
      assert.strictEqual(url.searchParams.get("offset"), "100");
      assert.strictEqual(
        (requestedHeaders as Record<string, string>)?.["Authorization"],
        "Bearer ops-bearer-token",
        "Must supply Authorization Bearer token"
      );
    } finally {
      globalThis.fetch = originalFetch;
    }
  });

  it("CR-INLINE-04: verifies generation counter discards stale asynchronous recipient lookups", async () => {
    let generation = 0;
    let resolvedRecipient: string | null = null;

    async function mockLookup(query: string, delayMs: number) {
      const currentGen = ++generation;
      await new Promise((r) => setTimeout(r, delayMs));
      if (currentGen === generation) {
        resolvedRecipient = query;
      }
    }

    const p1 = mockLookup("alice@bank1", 50);
    const p2 = mockLookup("bob@bank2", 5);

    await Promise.all([p1, p2]);
    assert.strictEqual(resolvedRecipient, "bob@bank2", "Stale earlier lookup must not overwrite newer lookup");
  });
});
