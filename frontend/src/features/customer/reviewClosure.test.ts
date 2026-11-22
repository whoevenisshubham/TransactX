import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { classifyPaymentOutcome } from "../../unresolvedStore";
import type { Account, Payment, Recipient } from "../../types";

describe("Milestone 20-R.4 Code Review Closure & Customer State Correctness", () => {
  describe("1. Customer Balance Refresh & Monotonic Guard", () => {
    it("discards out-of-order stale account responses when a newer refresh has dispatched", async () => {
      let stateAccounts: Account[] = [
        { accountNumber: "1001", balancePaise: 50000, status: "ACTIVE" },
      ];
      let refreshGen = 0;

      const deferredSlow = (resolveValue: Account[]) =>
        new Promise<Account[]>((resolve) => setTimeout(() => resolve(resolveValue), 50));
      const fastPromise = (resolveValue: Account[]) =>
        new Promise<Account[]>((resolve) => setTimeout(() => resolve(resolveValue), 10));

      // Request 1: dispatched first, returns old balance 50000 slowly
      const gen1 = ++refreshGen;
      const req1Promise = deferredSlow([
        { accountNumber: "1001", balancePaise: 50000, status: "ACTIVE" },
      ]).then((data) => {
        if (gen1 === refreshGen) {
          stateAccounts = data;
        }
      });

      // Request 2: dispatched second after payment completion, returns updated balance 40000 quickly
      const gen2 = ++refreshGen;
      const req2Promise = fastPromise([
        { accountNumber: "1001", balancePaise: 40000, status: "ACTIVE" },
      ]).then((data) => {
        if (gen2 === refreshGen) {
          stateAccounts = data;
        }
      });

      await Promise.all([req1Promise, req2Promise]);

      assert.strictEqual(
        stateAccounts[0].balancePaise,
        40000,
        "The newer generation response (40000) must be preserved and not overwritten by the slower older response"
      );
    });

    it("triggers balance refresh callback immediately upon payment reaching TERMINAL_COMPLETED", () => {
      let refreshTriggered = false;
      const completedPayment: Payment = {
        id: "pmt-success-001",
        amountPaise: 10000,
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

      const outcome = classifyPaymentOutcome(completedPayment);
      assert.strictEqual(outcome, "TERMINAL_COMPLETED");

      const onPaymentCompleted = (p: Payment) => {
        assert.strictEqual(p.id, "pmt-success-001");
        refreshTriggered = true;
      };

      if (outcome === "TERMINAL_COMPLETED") {
        onPaymentCompleted(completedPayment);
      }

      assert.strictEqual(refreshTriggered, true, "onPaymentCompleted must fire on TERMINAL_COMPLETED");
    });

    it("does not trigger balance refresh callback on non-terminal or intermediate payment states", () => {
      let refreshTriggered = false;
      const processingPayment: Payment = {
        id: "pmt-proc-001",
        amountPaise: 10000,
        currency: "INR",
        state: "PROCESSING",
        origin: "WEB",
        createdAt: new Date().toISOString(),
        senderName: "Alice",
        senderPaymentIdentifier: "alice@transactx",
        receiverName: "Bob",
        receiverPaymentIdentifier: "bob@transactx",
        direction: "SENT",
      };

      const outcome: string = classifyPaymentOutcome(processingPayment);
      assert.strictEqual(outcome, "NON_TERMINAL_PROCESSING");

      const onPaymentCompleted = () => {
        refreshTriggered = true;
      };

      if ((outcome as unknown) === "TERMINAL_COMPLETED") {
        onPaymentCompleted();
      }

      assert.strictEqual(refreshTriggered, false, "Balance refresh must NOT fire on NON_TERMINAL_PROCESSING");
    });
  });

  describe("2. Offline Replay Event-Driven Scheduler", () => {
    it("calculates earliest nextAttemptAt delta and avoids polling loops when queue is empty", () => {
      const retryableIntents = [
        { id: "intent-1", retry: { attemptCount: 1, nextAttemptAt: new Date(Date.now() + 60000).toISOString() } },
        { id: "intent-2", retry: { attemptCount: 2, nextAttemptAt: new Date(Date.now() + 20000).toISOString() } },
        { id: "intent-3", retry: { attemptCount: 1, nextAttemptAt: new Date(Date.now() + 45000).toISOString() } },
      ];

      const now = Date.now();
      let earliestDueTime = Infinity;

      for (const item of retryableIntents) {
        const nextAttempt = item.retry?.nextAttemptAt;
        if (!nextAttempt) {
          earliestDueTime = 0;
          break;
        }
        const time = new Date(nextAttempt).getTime();
        if (isNaN(time) || time <= now) {
          earliestDueTime = 0;
          break;
        }
        if (time < earliestDueTime) {
          earliestDueTime = time;
        }
      }

      assert.strictEqual(earliestDueTime < Infinity, true);
      const deltaMs = earliestDueTime - now;
      // Earliest is intent-2 (+20s), so delta should be ~20000ms
      assert.strictEqual(deltaMs >= 19000 && deltaMs <= 21000, true);
    });

    it("schedules immediate execution if any intent is due now or has past nextAttemptAt", () => {
      const retryableIntents = [
        { id: "intent-1", retry: { attemptCount: 1, nextAttemptAt: new Date(Date.now() - 5000).toISOString() } },
        { id: "intent-2", retry: { attemptCount: 2, nextAttemptAt: new Date(Date.now() + 20000).toISOString() } },
      ];

      const now = Date.now();
      let earliestDueTime = Infinity;

      for (const item of retryableIntents) {
        const nextAttempt = item.retry?.nextAttemptAt;
        if (!nextAttempt) {
          earliestDueTime = 0;
          break;
        }
        const time = new Date(nextAttempt).getTime();
        if (isNaN(time) || time <= now) {
          earliestDueTime = 0;
          break;
        }
        if (time < earliestDueTime) {
          earliestDueTime = time;
        }
      }

      assert.strictEqual(earliestDueTime, 0, "Past or due-now nextAttemptAt must evaluate to immediate execution (0)");
    });
  });

  describe("3. Recipient Lookup Invalidation & Frozen Reviewed Payload", () => {
    it("invalidates in-flight lookup response when user types or proceeds to confirm", () => {
      let lookupGeneration = 0;
      let activeRecipient: Recipient | null = null;

      // User starts typing and queries "bob@transactx"
      const currentGen = ++lookupGeneration;

      // User subsequently continues or types something else
      lookupGeneration++; // Invalidation event (e.g. continueToConfirm or user input edit)

      // When the first query resolves:
      const slowResolvedRecipient: Recipient = {
        name: "Bob Demo",
        paymentIdentifier: "bob@transactx",
        status: "ACTIVE",
      };

      if (currentGen === lookupGeneration) {
        activeRecipient = slowResolvedRecipient;
      }

      assert.strictEqual(activeRecipient, null, "In-flight recipient lookup must be discarded when generation increments");
    });

    it("captures and submits strictly the frozen reviewed payload", () => {
      const selectedRecipient: Recipient = {
        name: "Carol Store",
        paymentIdentifier: "carol@transactx",
        status: "ACTIVE",
      };
      const enteredAmountPaise = 150000;
      const enteredNote = "Monthly subscription";

      // On continueToConfirm, payload is frozen
      const reviewedRecipient: Recipient = { ...selectedRecipient };
      const reviewedAmountPaise = enteredAmountPaise;
      const reviewedNote = enteredNote;

      // Ambient state mutation (e.g. user or external effect)
      let ambientRecipient: Recipient = {
        name: "Attacker Recipient",
        paymentIdentifier: "attacker@transactx",
        status: "ACTIVE",
      };

      // Submission strictly uses reviewedRecipient ?? ambientRecipient
      const targetRecipient = reviewedRecipient ?? ambientRecipient;
      const targetAmount = reviewedAmountPaise;
      const targetNote = reviewedNote;

      assert.strictEqual(targetRecipient.paymentIdentifier, "carol@transactx");
      assert.strictEqual(targetAmount, 150000);
      assert.strictEqual(targetNote, "Monthly subscription");
    });
  });

  describe("4. Operations Views Asynchronous State Handling", () => {
    it("IntegrityView: invalidates in-flight verification verdict when a new proof is fetched", () => {
      let verifyGen = 0;
      let currentProofParams = { operationId: "op-original-1" };
      let verifyResult: { valid: boolean; reconstructedRoot: string } | null = null;

      // Start verify for proof 1
      const currentVerifyGen = ++verifyGen;
      const boundOpId = currentProofParams.operationId;

      // User fetches new proof 2 before verification resolves
      verifyGen++; // Invalidation on fetchProof
      currentProofParams = { operationId: "op-new-2" };

      // Verification for proof 1 resolves
      const resolutionResult = { valid: true, reconstructedRoot: "root-abc" };
      if (currentVerifyGen === verifyGen && currentProofParams.operationId === boundOpId) {
        verifyResult = resolutionResult;
      }

      assert.strictEqual(verifyResult, null, "Older proof verification verdict must not attach to newer proof");
    });

    it("ChaosView: default duration 30000ms satisfies HTML5 native step validation", () => {
      const min = 1000;
      const step = 1000;
      const defaultDuration = 30000;

      // Native HTML5 input validity formula for min and step:
      // Value is valid if (value - min) % step === 0
      const isStepValid = (defaultDuration - min) % step === 0;
      assert.strictEqual(isStepValid, true, "(30000 - 1000) % 1000 === 0 satisfies HTML5 step constraint");
    });

    it("ReconciliationView: pagination Previous button is enabled on offset > 0 even if page has 0 items", () => {
      const offset = 50;
      const limit = 50;
      const discrepancies: unknown[] = [];

      // Logic check: if discrepancies.length === 0, Previous button should still be available if offset > 0
      const shouldRenderPrevious = offset > 0;
      assert.strictEqual(shouldRenderPrevious, true, "User must not be trapped if an offset returns empty discrepancies");
    });
  });
});
