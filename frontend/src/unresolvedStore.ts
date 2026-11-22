import type { Recipient } from "./types";

export const UNRESOLVED_STORE_DB = "transactx-unresolved-intents";
export const UNRESOLVED_STORE_VERSION = 1;
export const UNRESOLVED_STORE_NAME = "unresolved-intents";

export type UnresolvedState = "UNCERTAIN" | "RESOLVED_COMPLETED" | "RESOLVED_FAILED";

export type DurableUnresolvedIntent = {
  readonly ownerUserId: string;
  readonly clientRequestId: string;
  readonly idempotencyKey: string;
  readonly recipient: Recipient;
  readonly amountPaise: number;
  readonly amount: string;
  readonly currency: string;
  readonly note: string;
  readonly payloadHash: string;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly state: UnresolvedState;
  readonly lastStatusCheckAt?: string;
  readonly lastHttpStatus?: number;
  readonly lastError?: string;
  readonly paymentId?: string;
  readonly persistenceFailed?: boolean;
};

export class DurableUnresolvedStore {
  private readonly dbName: string;
  private readonly ownerUserId: string;
  private readonly idbFactory: IDBFactory;
  private dbPromise: Promise<IDBDatabase> | null = null;
  private memoryFallback: Map<string, DurableUnresolvedIntent> = new Map();
  private memoryOnlyMode = false;
  private channel: BroadcastChannel | null = null;
  private changeListeners: Set<() => void> = new Set();

  constructor(ownerUserId: string, dbName = UNRESOLVED_STORE_DB, idbFactory = globalThis.indexedDB) {
    if (!ownerUserId.trim()) throw new Error("Durable unresolved store requires an authenticated owner");
    this.ownerUserId = ownerUserId;
    this.dbName = dbName;
    this.idbFactory = idbFactory;

    if (typeof BroadcastChannel !== "undefined") {
      try {
        this.channel = new BroadcastChannel(`transactx-unresolved-sync-${ownerUserId}`);
        this.channel.onmessage = (event) => {
          if (event.data?.type === "INTENT_CHANGED") {
            this.notifyListeners();
          }
        };
      } catch {
        this.channel = null;
      }
    }
  }

  onChanged(callback: () => void): () => void {
    this.changeListeners.add(callback);
    return () => {
      this.changeListeners.delete(callback);
    };
  }

  private notifyListeners(): void {
    for (const listener of this.changeListeners) {
      try {
        listener();
      } catch {
        // ignore listener errors
      }
    }
  }

  private broadcastChange(): void {
    this.notifyListeners();
    try {
      this.channel?.postMessage({ type: "INTENT_CHANGED", ownerUserId: this.ownerUserId });
    } catch {
      // ignore broadcast errors
    }
  }

  private async open(): Promise<IDBDatabase> {
    if (this.memoryOnlyMode || !this.idbFactory) {
      throw new Error("IndexedDB unavailable; operating in memory-only mode");
    }
    if (!this.dbPromise) {
      this.dbPromise = new Promise((resolve, reject) => {
        const request = this.idbFactory.open(this.dbName, UNRESOLVED_STORE_VERSION);
        request.onupgradeneeded = () => {
          const db = request.result;
          if (!db.objectStoreNames.contains(UNRESOLVED_STORE_NAME)) {
            const store = db.createObjectStore(UNRESOLVED_STORE_NAME, { keyPath: "clientRequestId" });
            store.createIndex("ownerUserId", "ownerUserId", { unique: false });
            store.createIndex("idempotencyKey", "idempotencyKey", { unique: false });
            store.createIndex("state", "state", { unique: false });
            store.createIndex("updatedAt", "updatedAt", { unique: false });
          }
        };
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => {
          this.memoryOnlyMode = true;
          reject(request.error ?? new Error("Failed to open unresolved intents IndexedDB"));
        };
      });
    }
    return this.dbPromise;
  }

  async save(intent: DurableUnresolvedIntent): Promise<DurableUnresolvedIntent> {
    if (intent.ownerUserId !== this.ownerUserId) {
      throw new Error("Tenant isolation violation: intent does not belong to store owner");
    }
    let persistedIntent = intent;
    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction(UNRESOLVED_STORE_NAME, "readwrite");
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error ?? new Error("Transaction failed"));
        tx.onabort = () => reject(tx.error ?? new Error("Transaction aborted"));
        tx.objectStore(UNRESOLVED_STORE_NAME).put(intent);
      });
      // also keep in memory
      this.memoryFallback.set(intent.clientRequestId, intent);
    } catch {
      // IndexedDB persistence failed (quota, disabled, private browsing)
      this.memoryOnlyMode = true;
      persistedIntent = { ...intent, persistenceFailed: true };
      this.memoryFallback.set(intent.clientRequestId, persistedIntent);
    }
    this.broadcastChange();
    return persistedIntent;
  }

  async getActive(): Promise<DurableUnresolvedIntent | null> {
    try {
      const db = await this.open();
      const records = await new Promise<DurableUnresolvedIntent[]>((resolve, reject) => {
        const tx = db.transaction(UNRESOLVED_STORE_NAME, "readonly");
        const store = tx.objectStore(UNRESOLVED_STORE_NAME);
        const request = store.index("ownerUserId").getAll(this.ownerUserId);
        request.onsuccess = () => resolve(request.result ?? []);
        request.onerror = () => reject(request.error ?? new Error("Read failed"));
      });
      // find latest UNCERTAIN intent for this owner
      const uncertain = records
        .filter((r) => r.ownerUserId === this.ownerUserId && r.state === "UNCERTAIN")
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
      if (uncertain.length > 0) {
        return uncertain[0];
      }
    } catch {
      // read from memory fallback if IndexedDB fails
      const memoryRecords = Array.from(this.memoryFallback.values())
        .filter((r) => r.ownerUserId === this.ownerUserId && r.state === "UNCERTAIN")
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
      if (memoryRecords.length > 0) {
        return memoryRecords[0];
      }
    }
    return null;
  }

  async get(clientRequestId: string): Promise<DurableUnresolvedIntent | null> {
    try {
      const db = await this.open();
      const record = await new Promise<DurableUnresolvedIntent | undefined>((resolve, reject) => {
        const tx = db.transaction(UNRESOLVED_STORE_NAME, "readonly");
        const request = tx.objectStore(UNRESOLVED_STORE_NAME).get(clientRequestId);
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error ?? new Error("Read failed"));
      });
      if (record && record.ownerUserId === this.ownerUserId) {
        return record;
      }
    } catch {
      const record = this.memoryFallback.get(clientRequestId);
      if (record && record.ownerUserId === this.ownerUserId) {
        return record;
      }
    }
    return null;
  }

  async markResolved(clientRequestId: string, state: "RESOLVED_COMPLETED" | "RESOLVED_FAILED", paymentId?: string): Promise<void> {
    const existing = await this.get(clientRequestId);
    if (!existing) return;
    const now = new Date().toISOString();
    const updated: DurableUnresolvedIntent = {
      ...existing,
      state,
      updatedAt: now,
      ...(paymentId ? { paymentId } : {}),
    };
    await this.save(updated);
  }

  async clearResolved(clientRequestId: string): Promise<void> {
    const existing = await this.get(clientRequestId);
    // Never delete a non-terminal UNCERTAIN intent!
    if (!existing || existing.state === "UNCERTAIN") return;

    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction(UNRESOLVED_STORE_NAME, "readwrite");
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
        tx.objectStore(UNRESOLVED_STORE_NAME).delete(clientRequestId);
      });
      this.memoryFallback.delete(clientRequestId);
    } catch {
      this.memoryFallback.delete(clientRequestId);
    }
    this.broadcastChange();
  }

  close(): void {
    if (this.dbPromise) {
      this.dbPromise.then((db) => db.close()).catch(() => undefined);
      this.dbPromise = null;
    }
    if (this.channel) {
      this.channel.close();
      this.channel = null;
    }
    this.changeListeners.clear();
  }
}
