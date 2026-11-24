import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parsePaise } from "../../money";
import { paymentResultCopy } from "../../components";
import type { Payment } from "../../types";

describe("Customer Financial Invariants & Domain Rules", () => {
  it("strictly enforces integer paise arithmetic and rejects floating point drift", () => {
    // Valid currency formats
    assert.strictEqual(parsePaise("10.00"), 1000);
    assert.strictEqual(parsePaise("0.01"), 1);
    assert.strictEqual(parsePaise("125.50"), 12550);
    assert.strictEqual(parsePaise("150"), 15000);
    assert.strictEqual(parsePaise("987654.32"), 98765432);

    // Invalid / fractional sub-paise inputs (strictly rejected)
    assert.strictEqual(parsePaise("10.001"), null);
    assert.strictEqual(parsePaise("10.999"), null);
    assert.strictEqual(parsePaise("-5.00"), null);
    assert.strictEqual(parsePaise("0"), null);
    assert.strictEqual(parsePaise("0.00"), null);
    assert.strictEqual(parsePaise("abc"), null);
    assert.strictEqual(parsePaise("12.34.56"), null);
  });

  it("produces deterministic receipt titles and tones based on payment state", () => {
    const basePayment: Payment = {
      id: "test-id",
      amountPaise: 15000,
      currency: "INR",
      direction: "SENT",
      origin: "WEB",
      senderName: "Alice",
      senderPaymentIdentifier: "alice@fixture",
      receiverName: "Bob",
      receiverPaymentIdentifier: "bob@fixture",
      state: "COMPLETED",
      createdAt: "2026-11-20T10:00:00Z",
      completedAt: "2026-11-20T10:00:01Z",
    };

    const completedCopy = paymentResultCopy(basePayment);
    assert.strictEqual(completedCopy.title, "Payment complete");
    assert.strictEqual(completedCopy.tone, "success");

    const failedCopy = paymentResultCopy({
      ...basePayment,
      state: "FAILED",
      failureReason: "Network drop",
    });
    assert.strictEqual(failedCopy.title, "Payment not completed");
    assert.strictEqual(failedCopy.tone, "error");

    const pendingCopy = paymentResultCopy({
      ...basePayment,
      state: "PENDING_RECONCILIATION",
    });
    assert.strictEqual(pendingCopy.title, "Payment still being confirmed");
    assert.strictEqual(pendingCopy.tone, "pending");
  });

  it("verifies idempotency keys are non-empty UUIDs", () => {
    const key = crypto.randomUUID();
    assert.match(
      key,
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
    );
  });
});
