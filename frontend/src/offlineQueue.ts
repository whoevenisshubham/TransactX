export const OFFLINE_QUEUE_DATABASE = "transactx-offline-queue";
export const OFFLINE_QUEUE_VERSION = 1;
export const OFFLINE_INTENTS_STORE = "offline-intents";

export type OfflineIntentState = "QUEUED" | "SYNCING" | "SYNCED" | "RETRYABLE" | "FAILED";

export type OfflinePaymentPayload = {
  recipient: string;
  amountPaise: number;
  currency: string;
  note?: string;
};

export type NormalizedOfflinePaymentPayload = {
  recipient: string;
  amountPaise: number;
  currency: string;
  note?: string;
};

export type RetryMetadata = {
  attemptCount: number;
  lastAttemptAt?: string;
  nextAttemptAt?: string;
  lastError?: string;
};

export type ReplayMetadata = {
  resolution?: "PENDING";
  paymentId?: string;
  paymentState?: string;
  lastHttpStatus?: number;
  lastResponseAt?: string;
};

export type OfflineIntent = {
  clientRequestId: string;
  idempotencyKey: string;
  payload: NormalizedOfflinePaymentPayload;
  payloadHash: string;
  createdAt: string;
  updatedAt: string;
  retry: RetryMetadata;
  replay?: ReplayMetadata;
  state: OfflineIntentState;
  leaseOwner?: string;
  leaseExpiresAt?: string;
};

export type QueueOptions = {
  clientRequestId?: string;
  idempotencyKey?: string;
  now?: Date;
};

export type IndexedDBFactory = IDBFactory;

const transitions: Record<OfflineIntentState, readonly OfflineIntentState[]> = {
  QUEUED: ["SYNCING", "RETRYABLE", "FAILED"],
  SYNCING: ["SYNCED", "RETRYABLE", "FAILED"],
  SYNCED: [],
  RETRYABLE: ["SYNCING", "FAILED"],
  FAILED: ["RETRYABLE"],
};

export function canTransition(from: OfflineIntentState, to: OfflineIntentState): boolean {
  return from === to || transitions[from].includes(to);
}

export function normalizePaymentPayload(payload: OfflinePaymentPayload): NormalizedOfflinePaymentPayload {
  const normalized: NormalizedOfflinePaymentPayload = {
    recipient: payload.recipient.trim(),
    amountPaise: payload.amountPaise,
    currency: payload.currency.trim().toUpperCase(),
  };
  const note = payload.note?.trim();
  if (note) normalized.note = note;
  return normalized;
}

export function stablePayloadString(payload: NormalizedOfflinePaymentPayload): string {
  return JSON.stringify({
    recipient: payload.recipient,
    amountPaise: payload.amountPaise,
    currency: payload.currency,
    ...(payload.note ? { note: payload.note } : {}),
  });
}

export async function hashPayload(payload: NormalizedOfflinePaymentPayload): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(stablePayloadString(payload)));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

function createID(): string {
  return crypto.randomUUID();
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("IndexedDB request failed"));
  });
}

function transactionComplete(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error ?? new Error("IndexedDB transaction failed"));
    transaction.onabort = () => reject(transaction.error ?? new Error("IndexedDB transaction aborted"));
  });
}

function createSchema(database: IDBDatabase): void {
  const store = database.createObjectStore(OFFLINE_INTENTS_STORE, { keyPath: "clientRequestId" });
  store.createIndex("state", "state", { unique: false });
  store.createIndex("updatedAt", "updatedAt", { unique: false });
  store.createIndex("nextAttemptAt", "retry.nextAttemptAt", { unique: false });
  store.createIndex("idempotencyKey", "idempotencyKey", { unique: true });
}

function upgradeSchema(database: IDBDatabase, transaction: IDBTransaction): void {
  if (!database.objectStoreNames.contains(OFFLINE_INTENTS_STORE)) {
    createSchema(database);
    return;
  }
  const store = transaction.objectStore(OFFLINE_INTENTS_STORE);
  if (!store.indexNames.contains("state")) store.createIndex("state", "state", { unique: false });
  if (!store.indexNames.contains("updatedAt")) store.createIndex("updatedAt", "updatedAt", { unique: false });
  if (!store.indexNames.contains("nextAttemptAt")) store.createIndex("nextAttemptAt", "retry.nextAttemptAt", { unique: false });
  if (!store.indexNames.contains("idempotencyKey")) store.createIndex("idempotencyKey", "idempotencyKey", { unique: true });
}

export class OfflineIntentQueue {
  private readonly databaseName: string;
  private readonly indexedDBFactory: IndexedDBFactory;
  private databasePromise: Promise<IDBDatabase> | null = null;

  constructor(databaseName = OFFLINE_QUEUE_DATABASE, indexedDBFactory = globalThis.indexedDB) {
    this.databaseName = databaseName;
    this.indexedDBFactory = indexedDBFactory;
  }

  private open(): Promise<IDBDatabase> {
    if (!this.databasePromise) {
      this.databasePromise = new Promise((resolve, reject) => {
        const request = this.indexedDBFactory.open(this.databaseName, OFFLINE_QUEUE_VERSION);
        request.onupgradeneeded = () => upgradeSchema(request.result, request.transaction!);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error ?? new Error("Unable to open offline queue"));
      });
    }
    return this.databasePromise;
  }

  async close(): Promise<void> {
    const database = await this.databasePromise;
    database?.close();
    this.databasePromise = null;
  }

  async enqueue(input: OfflinePaymentPayload, options: QueueOptions = {}): Promise<OfflineIntent> {
    const payload = normalizePaymentPayload(input);
    const now = (options.now ?? new Date()).toISOString();
    const intent: OfflineIntent = {
      clientRequestId: options.clientRequestId ?? createID(),
      idempotencyKey: options.idempotencyKey ?? createID(),
      payload,
      payloadHash: await hashPayload(payload),
      createdAt: now,
      updatedAt: now,
      retry: { attemptCount: 0 },
      state: "QUEUED",
    };
    const database = await this.open();
    const transaction = database.transaction(OFFLINE_INTENTS_STORE, "readwrite");
    transaction.objectStore(OFFLINE_INTENTS_STORE).add(intent);
    await transactionComplete(transaction);
    return intent;
  }

  async get(clientRequestId: string): Promise<OfflineIntent | undefined> {
    const database = await this.open();
    const transaction = database.transaction(OFFLINE_INTENTS_STORE, "readonly");
    return requestResult(transaction.objectStore(OFFLINE_INTENTS_STORE).get(clientRequestId));
  }

  async listByState(state: OfflineIntentState): Promise<OfflineIntent[]> {
    const database = await this.open();
    const transaction = database.transaction(OFFLINE_INTENTS_STORE, "readonly");
    return requestResult(transaction.objectStore(OFFLINE_INTENTS_STORE).index("state").getAll(state));
  }

  async listEligible(now = new Date()): Promise<OfflineIntent[]> {
    const [queued, retryable] = await Promise.all([this.listByState("QUEUED"), this.listByState("RETRYABLE")]);
    const timestamp = now.getTime();
    return [...queued, ...retryable]
      .filter((intent) => !intent.retry.nextAttemptAt || Date.parse(intent.retry.nextAttemptAt) <= timestamp)
      .sort((left, right) => left.createdAt.localeCompare(right.createdAt));
  }

  async updateState(clientRequestId: string, state: OfflineIntentState, now = new Date()): Promise<OfflineIntent> {
    return this.update(clientRequestId, (intent) => {
      if (!canTransition(intent.state, state)) throw new Error(`Invalid offline intent transition: ${intent.state} -> ${state}`);
      intent.state = state;
      intent.updatedAt = now.toISOString();
      if (state !== "SYNCING") {
        delete intent.leaseOwner;
        delete intent.leaseExpiresAt;
      }
    });
  }

  async recordRetry(clientRequestId: string, error: string, nextAttemptAt: Date, now = new Date(), metadata: ReplayMetadata = {}): Promise<OfflineIntent> {
    return this.update(clientRequestId, (intent) => {
      if (!canTransition(intent.state, "RETRYABLE")) throw new Error(`Invalid offline intent transition: ${intent.state} -> RETRYABLE`);
      intent.state = "RETRYABLE";
      intent.updatedAt = now.toISOString();
      intent.replay = { ...intent.replay, ...metadata, lastResponseAt: now.toISOString() };
      intent.retry = {
        attemptCount: intent.retry.attemptCount + 1,
        lastAttemptAt: now.toISOString(),
        nextAttemptAt: nextAttemptAt.toISOString(),
        lastError: error,
      };
      delete intent.leaseOwner;
      delete intent.leaseExpiresAt;
    });
  }

  async recordPending(clientRequestId: string, metadata: ReplayMetadata, error: string | undefined, nextAttemptAt: Date, now = new Date()): Promise<OfflineIntent> {
    return this.update(clientRequestId, (intent) => {
      if (!canTransition(intent.state, "RETRYABLE")) throw new Error(`Invalid offline intent transition: ${intent.state} -> RETRYABLE`);
      intent.state = "RETRYABLE";
      intent.updatedAt = now.toISOString();
      intent.replay = { ...intent.replay, ...metadata, resolution: "PENDING", lastResponseAt: now.toISOString() };
      intent.retry = {
        attemptCount: intent.retry.attemptCount + 1,
        lastAttemptAt: now.toISOString(),
        nextAttemptAt: nextAttemptAt.toISOString(),
        ...(error ? { lastError: error } : {}),
      };
      delete intent.leaseOwner;
      delete intent.leaseExpiresAt;
    });
  }

  async recordFailure(clientRequestId: string, error: string, metadata: ReplayMetadata = {}, now = new Date()): Promise<OfflineIntent> {
    return this.update(clientRequestId, (intent) => {
      if (!canTransition(intent.state, "FAILED")) throw new Error(`Invalid offline intent transition: ${intent.state} -> FAILED`);
      intent.state = "FAILED";
      intent.updatedAt = now.toISOString();
      intent.replay = { ...intent.replay, ...metadata, lastResponseAt: now.toISOString() };
      intent.retry = { ...intent.retry, lastAttemptAt: now.toISOString(), lastError: error };
      delete intent.leaseOwner;
      delete intent.leaseExpiresAt;
    });
  }

  async recordSynced(clientRequestId: string, metadata: ReplayMetadata, now = new Date()): Promise<OfflineIntent> {
    return this.update(clientRequestId, (intent) => {
      if (!canTransition(intent.state, "SYNCED")) throw new Error(`Invalid offline intent transition: ${intent.state} -> SYNCED`);
      intent.state = "SYNCED";
      intent.updatedAt = now.toISOString();
      intent.replay = { ...intent.replay, ...metadata, lastResponseAt: now.toISOString() };
      delete intent.leaseOwner;
      delete intent.leaseExpiresAt;
    });
  }

  async retryFailed(clientRequestId: string, now = new Date()): Promise<OfflineIntent> {
    return this.update(clientRequestId, (intent) => {
      if (!canTransition(intent.state, "RETRYABLE")) throw new Error(`Only failed intents can be retried: ${intent.state}`);
      intent.state = "RETRYABLE";
      intent.updatedAt = now.toISOString();
      intent.retry = { ...intent.retry, nextAttemptAt: now.toISOString(), lastError: undefined };
      delete intent.leaseOwner;
      delete intent.leaseExpiresAt;
    });
  }

  async claimEligible(owner: string, now = new Date(), leaseMilliseconds = 30_000): Promise<OfflineIntent | undefined> {
    const database = await this.open();
    const transaction = database.transaction(OFFLINE_INTENTS_STORE, "readwrite");
    const store = transaction.objectStore(OFFLINE_INTENTS_STORE);
    return new Promise((resolve, reject) => {
      let claimed: OfflineIntent | undefined;
      const request = store.getAll();
      request.onerror = () => reject(request.error ?? new Error("Unable to read eligible offline intents"));
      request.onsuccess = () => {
        const timestamp = now.getTime();
        claimed = request.result
          .filter((candidate) => {
            const retryReady = !candidate.retry.nextAttemptAt || Date.parse(candidate.retry.nextAttemptAt) <= timestamp;
            const leaseExpired = !candidate.leaseExpiresAt || Date.parse(candidate.leaseExpiresAt) <= timestamp;
            return (candidate.state === "QUEUED" || candidate.state === "RETRYABLE") && retryReady && leaseExpired || candidate.state === "SYNCING" && leaseExpired;
          })
          .sort((left, right) => left.createdAt.localeCompare(right.createdAt))[0];
        if (!claimed) return;
        claimed.state = "SYNCING";
        claimed.updatedAt = now.toISOString();
        claimed.leaseOwner = owner;
        claimed.leaseExpiresAt = new Date(timestamp + leaseMilliseconds).toISOString();
        store.put(claimed);
      };
      transaction.oncomplete = () => resolve(claimed);
      transaction.onerror = () => reject(transaction.error ?? new Error("Unable to claim offline intent"));
      transaction.onabort = () => reject(transaction.error ?? new Error("Offline intent claim aborted"));
    });
  }

  async removeIntent(clientRequestId: string): Promise<void> {
    const database = await this.open();
    const transaction = database.transaction(OFFLINE_INTENTS_STORE, "readwrite");
    const store = transaction.objectStore(OFFLINE_INTENTS_STORE);
    await new Promise<void>((resolve, reject) => {
      let settled = false;
      const fail = (error: Error) => {
        if (!settled) {
          settled = true;
          reject(error);
        }
        transaction.abort();
      };
      const request = store.get(clientRequestId);
      request.onerror = () => fail(request.error ?? new Error("Unable to read offline intent"));
      request.onsuccess = () => {
        const intent = request.result;
        if (!intent) {
          fail(new Error(`Offline intent not found: ${clientRequestId}`));
          return;
        }
        if (intent.state !== "SYNCED") {
          fail(new Error(`Cannot remove offline intent in state ${intent.state}`));
          return;
        }
        store.delete(clientRequestId);
      };
      transaction.oncomplete = () => {
        if (!settled) {
          settled = true;
          resolve();
        }
      };
      transaction.onerror = () => {
        if (!settled) {
          settled = true;
          reject(transaction.error ?? new Error("Unable to remove offline intent"));
        }
      };
      transaction.onabort = () => {
        if (!settled) {
          settled = true;
          reject(transaction.error ?? new Error("Offline intent removal aborted"));
        }
      };
    });
  }

  private async update(clientRequestId: string, mutate: (intent: OfflineIntent) => void): Promise<OfflineIntent> {
    const database = await this.open();
    const transaction = database.transaction(OFFLINE_INTENTS_STORE, "readwrite");
    const store = transaction.objectStore(OFFLINE_INTENTS_STORE);
    return new Promise((resolve, reject) => {
      let updated: OfflineIntent | undefined;
      const request = store.get(clientRequestId);
      request.onerror = () => reject(request.error ?? new Error("Unable to read offline intent"));
      request.onsuccess = () => {
        updated = request.result;
        if (!updated) {
          transaction.abort();
          return;
        }
        mutate(updated);
        store.put(updated);
      };
      transaction.oncomplete = () => {
        if (updated) resolve(updated);
        else reject(new Error(`Offline intent not found: ${clientRequestId}`));
      };
      transaction.onerror = () => reject(transaction.error ?? new Error("Unable to update offline intent"));
      transaction.onabort = () => reject(transaction.error ?? new Error(`Offline intent not found: ${clientRequestId}`));
    });
  }
}
