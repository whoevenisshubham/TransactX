import assert from "node:assert/strict";
import { after, before, test } from "node:test";
import { IDBFactory } from "fake-indexeddb";
import { OfflineIntentQueue, canTransition, hashPayload, normalizePaymentPayload, OFFLINE_QUEUE_VERSION, stablePayloadString } from "./offlineQueue";

const databaseName = `transactx-test-${crypto.randomUUID()}`;
const indexedDBFactory = new IDBFactory();
const payload = { recipient: " receiver@transactx ", amountPaise: 1250, currency: "inr", note: "  lunch  " };
let queue: OfflineIntentQueue;

before(() => { queue = new OfflineIntentQueue(databaseName, indexedDBFactory); });
after(async () => { await queue.close(); indexedDBFactory.deleteDatabase(databaseName); });

test("normalizes payloads and produces deterministic hashes", async () => {
  const normalized = normalizePaymentPayload(payload);
  assert.equal(stablePayloadString(normalized), '{"recipient":"receiver@transactx","amountPaise":1250,"currency":"INR","note":"lunch"}');
  assert.equal(await hashPayload(normalized), await hashPayload({ ...normalized }));
});

test("enqueue persists an intent and preserves generated identities after reopening", async () => {
  const intent = await queue.enqueue(payload, { now: new Date("2026-01-01T00:00:00.000Z") });
  await queue.close();
  queue = new OfflineIntentQueue(databaseName, indexedDBFactory);
  const reopened = await queue.get(intent.clientRequestId);
  assert.ok(reopened);
  if (!reopened) throw new Error("expected persisted intent");
  assert.equal(reopened.clientRequestId, intent.clientRequestId);
  assert.equal(reopened.idempotencyKey, intent.idempotencyKey);
  assert.equal(reopened.state, "QUEUED");
});

test("schema version and indexes are explicit", async () => {
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDBFactory.open(databaseName, OFFLINE_QUEUE_VERSION);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  const store = database.transaction("offline-intents", "readonly").objectStore("offline-intents");
  assert.equal(database.version, OFFLINE_QUEUE_VERSION);
  assert.deepEqual([...store.indexNames].sort(), ["idempotencyKey", "nextAttemptAt", "state", "updatedAt"]);
  database.close();
});

test("retry metadata, eligible lookup, and state transitions are durable", async () => {
  const intent = await queue.enqueue({ recipient: "retry@transactx", amountPaise: 500, currency: "INR" });
  const nextAttempt = new Date("2026-01-02T00:00:00.000Z");
  const retried = await queue.recordRetry(intent.clientRequestId, "network unavailable", nextAttempt, new Date("2026-01-01T00:00:00.000Z"));
  assert.equal(retried.retry.attemptCount, 1);
  assert.equal((await queue.listEligible(new Date("2026-01-01T23:00:00.000Z"))).some((candidate) => candidate.clientRequestId === intent.clientRequestId), false);
  assert.equal((await queue.listEligible(new Date("2026-01-02T00:00:00.000Z"))).some((candidate) => candidate.clientRequestId === intent.clientRequestId), true);
  const syncing = await queue.updateState(intent.clientRequestId, "SYNCING");
  assert.equal(syncing.state, "SYNCING");
  const failed = await queue.updateState(intent.clientRequestId, "FAILED");
  assert.equal(failed.state, "FAILED");
  assert.equal(canTransition("FAILED", "RETRYABLE"), true);
});

test("failed intents are retained and explicit deletion requires convergence state", async () => {
  const intent = await queue.enqueue({ recipient: "failed@transactx", amountPaise: 700, currency: "INR" });
  await queue.updateState(intent.clientRequestId, "FAILED");
  await assert.rejects(() => queue.removeIntent(intent.clientRequestId));
  assert.ok(await queue.get(intent.clientRequestId));
  const synced = await queue.enqueue({ recipient: "synced@transactx", amountPaise: 800, currency: "INR" });
  await queue.updateState(synced.clientRequestId, "SYNCING");
  await queue.updateState(synced.clientRequestId, "SYNCED");
  await queue.removeIntent(synced.clientRequestId);
  assert.equal(await queue.get(synced.clientRequestId), undefined);
});

test("duplicate idempotency keys are rejected without changing the original intent", async () => {
  const original = await queue.enqueue({ recipient: "original@transactx", amountPaise: 900, currency: "INR" }, { idempotencyKey: "idem-duplicate-test" });
  await assert.rejects(() => queue.enqueue({ recipient: "duplicate@transactx", amountPaise: 901, currency: "INR" }, { idempotencyKey: original.idempotencyKey }));
  assert.deepEqual(await queue.get(original.clientRequestId), original);
});

test("expired syncing leases are reclaimable but active leases cannot be stolen", async () => {
  const leaseQueueName = `transactx-lease-test-${crypto.randomUUID()}`;
  const leaseQueue = new OfflineIntentQueue(leaseQueueName, indexedDBFactory);
  try {
    const intent = await leaseQueue.enqueue({ recipient: "claim@transactx", amountPaise: 900, currency: "INR" });
    const initialTime = new Date("2026-01-03T00:00:00.000Z");
    const firstClaim = await leaseQueue.claimEligible("worker-a", initialTime, 1_000);
    assert.equal(firstClaim?.state, "SYNCING");
    assert.equal(firstClaim?.leaseOwner, "worker-a");
    const activeClaim = await leaseQueue.claimEligible("worker-b", new Date("2026-01-03T00:00:00.999Z"), 1_000);
    assert.equal(activeClaim, undefined);
    const reclaimed = await leaseQueue.claimEligible("worker-b", new Date("2026-01-03T00:00:01.001Z"), 1_000);
    assert.equal(reclaimed?.clientRequestId, intent.clientRequestId);
    assert.equal(reclaimed?.state, "SYNCING");
    assert.equal(reclaimed?.leaseOwner, "worker-b");
    const stored = await leaseQueue.get(intent.clientRequestId);
    assert.equal(stored?.leaseOwner, "worker-b");
    assert.equal(stored?.state, "SYNCING");
  } finally {
    await leaseQueue.close();
    indexedDBFactory.deleteDatabase(leaseQueueName);
  }
});

test("public queue operations complete and persist their results", async () => {
  const operationQueueName = `transactx-operation-test-${crypto.randomUUID()}`;
  const operationQueue = new OfflineIntentQueue(operationQueueName, indexedDBFactory);
  try {
    const intent = await operationQueue.enqueue({ recipient: "operations@transactx", amountPaise: 1_100, currency: "INR" });
    const syncing = await operationQueue.updateState(intent.clientRequestId, "SYNCING");
    assert.equal((await operationQueue.get(intent.clientRequestId))?.state, "SYNCING");
    const retryable = await operationQueue.recordRetry(intent.clientRequestId, "temporary failure", new Date("2026-01-04T00:00:10.000Z"), new Date("2026-01-04T00:00:00.000Z"));
    assert.equal(retryable.state, "RETRYABLE");
    const claimed = await operationQueue.claimEligible("operation-worker", new Date("2026-01-04T00:00:11.000Z"));
    assert.equal(claimed?.clientRequestId, syncing.clientRequestId);
    const synced = await operationQueue.updateState(intent.clientRequestId, "SYNCED");
    assert.equal(synced.state, "SYNCED");
    await operationQueue.removeIntent(intent.clientRequestId);
    assert.equal(await operationQueue.get(intent.clientRequestId), undefined);
  } finally {
    await operationQueue.close();
    indexedDBFactory.deleteDatabase(operationQueueName);
  }
});
