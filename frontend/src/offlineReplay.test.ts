import assert from "node:assert/strict";
import { after, test } from "node:test";
import { IDBFactory } from "fake-indexeddb";
import { OfflineIntentQueue } from "./offlineQueue";
import { OfflineReplayWorker, retryDelayMs, type ReplayClient } from "./offlineReplay";
import type { Payment } from "./types";

const indexedDBFactory = new IDBFactory();

function payment(id: string, state: string): Payment {
  return { id, amountPaise: 1200, currency: "INR", origin: "ONLINE", state, createdAt: "2026-01-01T00:00:00.000Z", senderName: "Payer", senderPaymentIdentifier: "payer@transactx", receiverName: "Receiver", receiverPaymentIdentifier: "receiver@transactx", direction: "SENT" };
}

async function withQueue(run: (queue: OfflineIntentQueue) => Promise<void>): Promise<void> {
  const name = `transactx-replay-test-${crypto.randomUUID()}`;
  const queue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  try { await run(queue); } finally { await queue.close(); indexedDBFactory.deleteDatabase(name); }
}

after(() => undefined);

class LeaseLossQueue extends OfflineIntentQueue {
  override async renewLease(): Promise<boolean> {
    return false;
  }
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

test("successful replay uses the original identities and converges to SYNCED", async () => {
  await withQueue(async (queue) => {
    const intent = await queue.enqueue({ recipient: "receiver@transactx", amountPaise: 1200, currency: "INR" }, { clientRequestId: "client-success", idempotencyKey: "idem-success", now: new Date("2026-01-01T00:00:00.000Z") });
    assert.equal(intent.ownerUserId, "user-a");
    const calls: Array<[string, string]> = [];
    const client: ReplayClient = { createPayment: async (_payload, key, clientRequestId) => { calls.push([key, clientRequestId]); return { payment: payment("payment-success", "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const result = await new OfflineReplayWorker(queue, client, "worker-success").run(new Date("2026-01-01T00:00:01.000Z"));
    assert.deepEqual(calls, [[intent.idempotencyKey, intent.clientRequestId]]);
    assert.equal(result[0].outcome, "SYNCED");
    assert.equal((await queue.get(intent.clientRequestId))?.state, "SYNCED");
  });
});

test("202 pending responses retain the payment reference and resolve by status before replay", async () => {
  await withQueue(async (queue) => {
    const intent = await queue.enqueue({ recipient: "pending@transactx", amountPaise: 1200, currency: "INR" });
    let postCalls = 0;
    let statusCalls = 0;
    const client: ReplayClient = { createPayment: async () => { postCalls += 1; return { payment: payment("payment-pending", "PROCESSING"), status: 202 }; }, getPayment: async () => { statusCalls += 1; return payment("payment-pending", "COMPLETED"); } };
    const worker = new OfflineReplayWorker(queue, client, "worker-pending");
    const first = await worker.run(new Date("2026-01-01T00:00:00.000Z"));
    const pending = await queue.get(intent.clientRequestId);
    assert.equal(first[0].outcome, "PENDING");
    assert.equal(pending?.state, "RETRYABLE");
    assert.equal(pending?.replay?.paymentId, "payment-pending");
    assert.equal(pending?.replay?.resolution, "PENDING");
    await worker.run(new Date("2026-01-01T00:01:00.000Z"));
    assert.equal(postCalls, 1);
    assert.equal(statusCalls, 1);
    assert.equal((await queue.get(intent.clientRequestId))?.state, "SYNCED");
  });
});

test("lost response is retained as pending and a later replay reuses the same identity", async () => {
  await withQueue(async (queue) => {
    const intent = await queue.enqueue({ recipient: "unknown@transactx", amountPaise: 1200, currency: "INR" }, { idempotencyKey: "idem-unknown" });
    let calls = 0;
    const client: ReplayClient = { createPayment: async (_payload, key, clientRequestId) => { calls += 1; assert.equal(key, "idem-unknown"); assert.equal(clientRequestId, intent.clientRequestId); if (calls === 1) throw Object.assign(new Error("network lost"), { status: 0, code: "NETWORK_ERROR" }); return { payment: payment("payment-recovered", "COMPLETED"), status: 200 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const worker = new OfflineReplayWorker(queue, client, "worker-unknown");
    await worker.run(new Date("2026-01-01T00:00:00.000Z"));
    assert.equal((await queue.get(intent.clientRequestId))?.replay?.resolution, "PENDING");
    await worker.run(new Date("2026-01-01T00:01:00.000Z"));
    assert.equal(calls, 2);
    assert.equal((await queue.get(intent.clientRequestId))?.state, "SYNCED");
  });
});

test("retryable and permanent failures are classified and failed retry preserves identity", async () => {
  await withQueue(async (queue) => {
    const retryable = await queue.enqueue({ recipient: "retry@transactx", amountPaise: 1200, currency: "INR" });
    const permanent = await queue.enqueue({ recipient: "bad@transactx", amountPaise: 1300, currency: "INR" });
    const calls: string[] = [];
    const attempts = new Map<string, number>();
    const client: ReplayClient = { createPayment: async (payload, key) => { calls.push(payload.recipient + ":" + key); const attempt = (attempts.get(payload.recipient) ?? 0) + 1; attempts.set(payload.recipient, attempt); if (payload.recipient === "retry@transactx" && attempt === 1) throw Object.assign(new Error("temporarily unavailable"), { status: 503, code: "BANK_UNAVAILABLE" }); if (payload.recipient === "bad@transactx") throw Object.assign(new Error("invalid request"), { status: 400, code: "INVALID_REQUEST" }); return { payment: payment("payment-retry", "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const worker = new OfflineReplayWorker(queue, client, "worker-failures");
    await worker.run(new Date("2026-01-01T00:00:00.000Z"));
    assert.equal((await queue.get(retryable.clientRequestId))?.state, "RETRYABLE");
    assert.equal((await queue.get(permanent.clientRequestId))?.state, "FAILED");
    await queue.retryFailed(permanent.clientRequestId, new Date("2026-01-01T00:01:00.000Z"));
    const retriedPermanent = await queue.get(permanent.clientRequestId);
    assert.equal(retriedPermanent?.idempotencyKey, permanent.idempotencyKey);
    assert.equal(retriedPermanent?.clientRequestId, permanent.clientRequestId);
  });
});

test("replay ordering is oldest-first and concurrent workers submit one intent once", async () => {
  await withQueue(async (queue) => {
    const first = await queue.enqueue({ recipient: "first@transactx", amountPaise: 100, currency: "INR" }, { now: new Date("2026-01-01T00:00:00.000Z") });
    const second = await queue.enqueue({ recipient: "second@transactx", amountPaise: 200, currency: "INR" }, { now: new Date("2026-01-01T00:00:01.000Z") });
    const order: string[] = [];
    const client: ReplayClient = { createPayment: async (payload) => { order.push(payload.recipient); return { payment: payment(`payment-${payload.recipient}`, "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const [left, right] = await Promise.all([new OfflineReplayWorker(queue, client, "worker-a").run(new Date("2026-01-01T00:00:02.000Z")), new OfflineReplayWorker(queue, client, "worker-b").run(new Date("2026-01-01T00:00:02.000Z"))]);
    assert.deepEqual(order, ["first@transactx", "second@transactx"]);
    assert.equal(left.length + right.length, 2);
    assert.equal((await queue.get(first.clientRequestId))?.state, "SYNCED");
    assert.equal((await queue.get(second.clientRequestId))?.state, "SYNCED");
  });
});

test("retry backoff is deterministic and bounded", () => {
  assert.equal(retryDelayMs(1), 30_000);
  assert.equal(retryDelayMs(2), 60_000);
  assert.equal(retryDelayMs(20), 30 * 60 * 1000);
});

test("a different user replay worker cannot claim or submit another user's intent", async () => {
  const name = `transactx-replay-owner-${crypto.randomUUID()}`;
  const ownerQueue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  const otherQueue = new OfflineIntentQueue(name, "user-b", indexedDBFactory);
  try {
    const intent = await ownerQueue.enqueue({ recipient: "owner@transactx", amountPaise: 100, currency: "INR" });
    let submitted = false;
    const client: ReplayClient = { createPayment: async () => { submitted = true; return { payment: payment("payment-owner", "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const result = await new OfflineReplayWorker(otherQueue, client, "worker-b").run();
    assert.deepEqual(result, []);
    assert.equal(submitted, false);
    assert.equal((await ownerQueue.get(intent.clientRequestId))?.state, "QUEUED");
  } finally { await ownerQueue.close(); await otherQueue.close(); }
});

test("a long replay renews its lease and stops the heartbeat after completion", async () => {
  const name = `transactx-replay-heartbeat-${crypto.randomUUID()}`;
  class CountingQueue extends OfflineIntentQueue {
    renewals = 0;
    override async renewLease(clientRequestId: string, leaseOwner: string, now = new Date(), leaseMilliseconds = 30_000): Promise<boolean> { this.renewals += 1; return super.renewLease(clientRequestId, leaseOwner, now, leaseMilliseconds); }
  }
  const firstQueue = new CountingQueue(name, "user-a", indexedDBFactory);
  const secondQueue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  try {
    const intent = await firstQueue.enqueue({ recipient: "slow@transactx", amountPaise: 100, currency: "INR" });
    let releaseRequest!: () => void;
    const requestReleased = new Promise<void>((resolve) => { releaseRequest = resolve; });
    let calls = 0;
    const client: ReplayClient = { createPayment: async () => { calls += 1; await requestReleased; return { payment: payment("payment-slow", "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const workerPromise = new OfflineReplayWorker(firstQueue, client, "worker-a", 30).run(new Date());
    await new Promise((resolve) => setTimeout(resolve, 45));
    assert.equal(await secondQueue.claimEligible("worker-b", new Date(), 30), undefined);
    releaseRequest();
    await workerPromise;
    assert.equal(calls, 1);
    assert.ok(firstQueue.renewals > 0);
    assert.equal((await firstQueue.get(intent.clientRequestId))?.state, "SYNCED");
    const renewalsAfterCompletion = firstQueue.renewals;
    await new Promise((resolve) => setTimeout(resolve, 45));
    assert.equal(firstQueue.renewals, renewalsAfterCompletion);
    assert.equal((await firstQueue.get(intent.clientRequestId))?.state, "SYNCED");
  } finally { await firstQueue.close(); await secondQueue.close(); }
});

test("create replay fails closed when its lease is lost during the request", async () => {
  const name = `transactx-lease-loss-create-${crypto.randomUUID()}`;
  const queue = new LeaseLossQueue(name, "user-a", indexedDBFactory);
  const recoveryQueue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  try {
    const intent = await queue.enqueue({ recipient: "lease-loss@transactx", amountPaise: 100, currency: "INR" }, { idempotencyKey: "lease-loss-idem" });
    let release!: () => void;
    const request = new Promise<void>((resolve) => { release = resolve; });
    const client: ReplayClient = { createPayment: async (_payload, key, clientRequestId) => { assert.equal(key, intent.idempotencyKey); assert.equal(clientRequestId, intent.clientRequestId); await request; return { payment: payment("late-payment", "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const run = new OfflineReplayWorker(queue, client, "worker-a", 20).run();
    await delay(15);
    release();
    const result = await run;
    const stored = await queue.get(intent.clientRequestId);
    assert.equal(result[0].outcome, "PENDING");
    assert.equal(stored?.state, "SYNCING");
    assert.notEqual(stored?.state, "FAILED");
    assert.equal(stored?.idempotencyKey, intent.idempotencyKey);
    assert.equal(stored?.clientRequestId, intent.clientRequestId);
    assert.equal((await recoveryQueue.claimEligible("worker-b", new Date(Date.now() + 30)))?.clientRequestId, intent.clientRequestId);
  } finally { await queue.close(); await recoveryQueue.close(); indexedDBFactory.deleteDatabase(name); }
});

test("status lookup lease loss is also non-final and never FAILED", async () => {
  const name = `transactx-lease-loss-status-${crypto.randomUUID()}`;
  const seedQueue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  const queue = new LeaseLossQueue(name, "user-a", indexedDBFactory);
  try {
    const intent = await seedQueue.enqueue({ recipient: "status-loss@transactx", amountPaise: 100, currency: "INR" }, { idempotencyKey: "status-loss-idem" });
    const seedTime = new Date();
    await seedQueue.claimEligible("seed", seedTime, 1_000);
    await seedQueue.recordPending(intent.clientRequestId, { paymentId: "authoritative-payment", paymentState: "PROCESSING" }, undefined, new Date(seedTime.getTime() + 60_000), seedTime, "seed");
    let release!: () => void;
    const request = new Promise<void>((resolve) => { release = resolve; });
    const client: ReplayClient = { createPayment: async () => { throw new Error("must use status lookup"); }, getPayment: async () => { await request; return payment("authoritative-payment", "COMPLETED"); } };
    const run = new OfflineReplayWorker(queue, client, "worker-a", 20).run(new Date(seedTime.getTime() + 60_000));
    await delay(15);
    release();
    const result = await run;
    const stored = await queue.get(intent.clientRequestId);
    assert.equal(result[0].outcome, "PENDING");
    assert.equal(stored?.state, "SYNCING");
    assert.notEqual(stored?.state, "FAILED");
    assert.equal(stored?.idempotencyKey, intent.idempotencyKey);
    assert.equal(stored?.clientRequestId, intent.clientRequestId);
  } finally { await seedQueue.close(); await queue.close(); indexedDBFactory.deleteDatabase(name); }
});

test("stopping a worker during an active request leaves the intent recoverable", async () => {
  const name = `transactx-lease-loss-stop-${crypto.randomUUID()}`;
  const queue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  const recoveryQueue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  try {
    const intent = await queue.enqueue({ recipient: "stop-loss@transactx", amountPaise: 100, currency: "INR" });
    let release!: () => void;
    const request = new Promise<void>((resolve) => { release = resolve; });
    const client: ReplayClient = { createPayment: async () => { await request; return { payment: payment("stopped-payment", "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const worker = new OfflineReplayWorker(queue, client, "worker-stop", 20);
    const run = worker.run();
    await delay(5);
    worker.stop();
    release();
    const result = await run;
    assert.equal(result[0].outcome, "PENDING");
    assert.equal((await queue.get(intent.clientRequestId))?.state, "SYNCING");
    await delay(25);
    assert.equal((await recoveryQueue.claimEligible("worker-recovery", new Date(Date.now() + 30)))?.clientRequestId, intent.clientRequestId);
  } finally { await queue.close(); await recoveryQueue.close(); indexedDBFactory.deleteDatabase(name); }
});

test("stale worker cannot overwrite a newer worker after lease reclaim", async () => {
  const name = `transactx-lease-loss-race-${crypto.randomUUID()}`;
  const staleQueue = new LeaseLossQueue(name, "user-a", indexedDBFactory);
  const recoveryQueue = new OfflineIntentQueue(name, "user-a", indexedDBFactory);
  try {
    const intent = await staleQueue.enqueue({ recipient: "race-loss@transactx", amountPaise: 100, currency: "INR" });
    let release!: () => void;
    const request = new Promise<void>((resolve) => { release = resolve; });
    const client: ReplayClient = { createPayment: async () => { await request; return { payment: payment("stale-payment", "COMPLETED"), status: 201 }; }, getPayment: async () => payment("unused", "COMPLETED") };
    const staleRun = new OfflineReplayWorker(staleQueue, client, "worker-stale", 20).run();
    await delay(30);
    const reclaimed = await recoveryQueue.claimEligible("worker-new", new Date(Date.now() + 30), 1_000);
    assert.equal(reclaimed?.leaseOwner, "worker-new");
    release();
    const result = await staleRun;
    assert.equal(result[0].outcome, "PENDING");
    const stored = await recoveryQueue.get(intent.clientRequestId);
    assert.equal(stored?.leaseOwner, "worker-new");
    assert.equal(stored?.state, "SYNCING");
    assert.notEqual(stored?.state, "FAILED");
  } finally { await staleQueue.close(); await recoveryQueue.close(); indexedDBFactory.deleteDatabase(name); }
});