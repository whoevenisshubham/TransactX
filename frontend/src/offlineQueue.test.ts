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
  await queue.removeIntent(intent.clientRequestId, "FAILED");
  assert.equal(await queue.get(intent.clientRequestId), undefined);
});

test("atomic claims prevent concurrent workers from claiming the same intent", async () => {
  const intent = await queue.enqueue({ recipient: "claim@transactx", amountPaise: 900, currency: "INR" });
  const claims = await Promise.all([queue.claimEligible("worker-a"), queue.claimEligible("worker-b")]);
  const claimed = claims.filter((candidate) => candidate?.clientRequestId === intent.clientRequestId);
  assert.equal(claimed.length, 1);
  assert.equal((await queue.get(intent.clientRequestId))?.state, "SYNCING");
});