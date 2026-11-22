import type { Recipient } from "./types";

export const UNRESOLVED_STORE_DB = "transactx-unresolved-intents";
export const UNRESOLVED_STORE_VERSION = 2;
export const UNRESOLVED_STORE_NAME = "unresolved-intents";
export const ACTIVE_RESERVATIONS_STORE_NAME = "active-reservations";

export type UnresolvedState =
  | "PREPARED"
  | "DISPATCHING"
  | "PROCESSING"
  | "UNCERTAIN"
  | "RESOLVED_COMPLETED"
  | "RESOLVED_FAILED"
  | "RESOLVED_REVERSED";

export type PaymentOutcomeCategory =
  | "TERMINAL_COMPLETED"
  | "TERMINAL_FAILED"
  | "TERMINAL_REVERSED"
  | "NON_TERMINAL_PROCESSING"
  | "UNKNOWN_STATE";

export type ActiveReservation = {
  readonly ownerUserId: string;
  readonly clientRequestId: string;
  readonly idempotencyKey: string;
  readonly state: UnresolvedState;
  readonly createdAt: string;
  readonly updatedAt: string;
};

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

export class StorageUnavailableError extends Error {
  constructor(message = "Durable browser storage (IndexedDB) is unavailable or disabled.") {
    super(message);
    this.name = "StorageUnavailableError";
  }
}

export class ActiveReservationConflictError extends Error {
  readonly existingReservation: ActiveReservation;

  constructor(
    message: string,
    existingReservation: ActiveReservation
  ) {
    super(message);
    this.name = "ActiveReservationConflictError";
    this.existingReservation = existingReservation;
  }
}

export function isTerminalState(state: UnresolvedState): boolean {
  return (
    state === "RESOLVED_COMPLETED" ||
    state === "RESOLVED_FAILED" ||
    state === "RESOLVED_REVERSED"
  );
}

export function isTerminalOutcome(category: PaymentOutcomeCategory): boolean {
  return (
    category === "TERMINAL_COMPLETED" ||
    category === "TERMINAL_FAILED" ||
    category === "TERMINAL_REVERSED"
  );
}

export function classifyPaymentOutcome(
  paymentOrStatus: unknown
): PaymentOutcomeCategory {
  let rawState: string | undefined;
  if (typeof paymentOrStatus === "string") {
    rawState = paymentOrStatus;
  } else if (paymentOrStatus && typeof paymentOrStatus === "object") {
    const obj = paymentOrStatus as Record<string, unknown>;
    rawState = (obj.state as string) ?? (obj.status as string);
  }
  const state = (rawState ?? "").toUpperCase().trim();
  switch (state) {
    case "COMPLETED":
      return "TERMINAL_COMPLETED";
    case "FAILED":
      return "TERMINAL_FAILED";
    case "REVERSED":
      return "TERMINAL_REVERSED";
    case "CREATED":
    case "VALIDATING":
    case "LOCAL_SETTLEMENT":
    case "ROUTING":
    case "PROCESSING":
    case "COMMITTED":
    case "BANK_SETTLED_CENTRAL_PENDING":
    case "PENDING_RECONCILIATION":
    case "OFFLINE_CAPTURED":
    case "QUEUED":
    case "SYNCING":
    case "REPLAY_FAILED":
      return "NON_TERMINAL_PROCESSING";
    default:
      return "UNKNOWN_STATE";
  }
}

export async function computePayloadHash(payload: Record<string, unknown>): Promise<string> {
  const sortedKeys = Object.keys(payload).sort();
  const canonicalObj: Record<string, unknown> = {};
  for (const key of sortedKeys) {
    if (payload[key] !== undefined) {
      canonicalObj[key] = payload[key];
    }
  }
  const canonicalString = JSON.stringify(canonicalObj);

  if (typeof globalThis.crypto?.subtle?.digest === "function") {
    const encoder = new TextEncoder();
    const data = encoder.encode(canonicalString);
    const hashBuffer = await globalThis.crypto.subtle.digest("SHA-256", data);
    const hashArray = Array.from(new Uint8Array(hashBuffer));
    return hashArray.map((b) => b.toString(16).padStart(2, "0")).join("");
  }

  // Deterministic fallback for non-subtle environments
  let hash = 0x811c9dc5;
  for (let i = 0; i < canonicalString.length; i++) {
    hash ^= canonicalString.charCodeAt(i);
    hash += (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
  }
  return `fallback-${(hash >>> 0).toString(16).padStart(8, "0")}`;
}

export class DurableUnresolvedStore {
  private readonly dbName: string;
  private readonly ownerUserId: string;
  private readonly idbFactory?: IDBFactory;
  private readonly allowMemoryFallback: boolean;
  private dbPromise: Promise<IDBDatabase> | null = null;
  private memoryFallback: Map<string, DurableUnresolvedIntent> = new Map();
  private memoryReservations: Map<string, ActiveReservation> = new Map();
  private memoryOnlyMode = false;
  private channel: BroadcastChannel | null = null;
  private changeListeners: Set<() => void> = new Set();

  constructor(
    ownerUserId: string,
    dbName = UNRESOLVED_STORE_DB,
    idbFactory: IDBFactory | undefined = globalThis.indexedDB,
    allowMemoryFallback = false
  ) {
    if (!ownerUserId.trim()) {
      throw new Error("Durable unresolved store requires an authenticated owner");
    }
    this.ownerUserId = ownerUserId;
    this.dbName = dbName;
    this.idbFactory = idbFactory;
    this.allowMemoryFallback = allowMemoryFallback;

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

  getOwnerUserId(): string {
    return this.ownerUserId;
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
      this.channel?.postMessage({
        type: "INTENT_CHANGED",
        ownerUserId: this.ownerUserId,
      });
    } catch {
      // ignore broadcast errors
    }
  }

  private async open(): Promise<IDBDatabase> {
    if (!this.idbFactory) {
      if (this.allowMemoryFallback) {
        this.memoryOnlyMode = true;
        throw new Error("IndexedDB unavailable; operating in memory-only mode");
      }
      throw new StorageUnavailableError("IndexedDB factory is not available in this environment.");
    }

    if (this.memoryOnlyMode) {
      if (this.allowMemoryFallback) {
        throw new Error("IndexedDB unavailable; operating in memory-only mode");
      }
      throw new StorageUnavailableError("IndexedDB is in failed state.");
    }

    if (!this.dbPromise) {
      this.dbPromise = new Promise((resolve, reject) => {
        const request = this.idbFactory!.open(this.dbName, UNRESOLVED_STORE_VERSION);
        request.onupgradeneeded = () => {
          const db = request.result;
          if (!db.objectStoreNames.contains(UNRESOLVED_STORE_NAME)) {
            const store = db.createObjectStore(UNRESOLVED_STORE_NAME, {
              keyPath: "clientRequestId",
            });
            store.createIndex("ownerUserId", "ownerUserId", { unique: false });
            store.createIndex("idempotencyKey", "idempotencyKey", { unique: false });
            store.createIndex("state", "state", { unique: false });
            store.createIndex("updatedAt", "updatedAt", { unique: false });
          }
          if (!db.objectStoreNames.contains(ACTIVE_RESERVATIONS_STORE_NAME)) {
            db.createObjectStore(ACTIVE_RESERVATIONS_STORE_NAME, {
              keyPath: "ownerUserId",
            });
          }
        };
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => {
          this.memoryOnlyMode = true;
          const err = request.error ?? new Error("Failed to open unresolved intents IndexedDB");
          reject(
            this.allowMemoryFallback
              ? err
              : new StorageUnavailableError(err.message)
          );
        };
      });
    }
    return this.dbPromise;
  }

  /**
   * Pre-dispatch atomic reservation.
   * Atomically reserves the user's payment identity in IndexedDB BEFORE any HTTP dispatch.
   * If another active payment reservation exists for this user, throws ActiveReservationConflictError.
   * If storage is unavailable and allowMemoryFallback is false, fails closed with StorageUnavailableError.
   */
  async reserveActiveIntent(intent: DurableUnresolvedIntent): Promise<void> {
    if (intent.ownerUserId !== this.ownerUserId) {
      throw new Error("Tenant isolation violation: intent does not belong to store owner");
    }

    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction(
          [ACTIVE_RESERVATIONS_STORE_NAME, UNRESOLVED_STORE_NAME],
          "readwrite"
        );
        const activeStore = tx.objectStore(ACTIVE_RESERVATIONS_STORE_NAME);
        const intentStore = tx.objectStore(UNRESOLVED_STORE_NAME);

        const checkReq = activeStore.get(this.ownerUserId);
        checkReq.onerror = () => reject(checkReq.error ?? new Error("Active reservation check failed"));
        checkReq.onsuccess = () => {
          const existing = checkReq.result as ActiveReservation | undefined;
          if (
            existing &&
            existing.clientRequestId !== intent.clientRequestId &&
            !isTerminalState(existing.state)
          ) {
            reject(
              new ActiveReservationConflictError(
                "Another payment reservation is already active for this account.",
                existing
              )
            );
            return;
          }

          const reservation: ActiveReservation = {
            ownerUserId: this.ownerUserId,
            clientRequestId: intent.clientRequestId,
            idempotencyKey: intent.idempotencyKey,
            state: intent.state,
            createdAt: intent.createdAt,
            updatedAt: intent.updatedAt,
          };

          activeStore.put(reservation);
          intentStore.put(intent);
        };

        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error ?? new Error("Reservation transaction failed"));
        tx.onabort = () => reject(tx.error ?? new Error("Reservation transaction aborted"));
      });

      const resRecord: ActiveReservation = {
        ownerUserId: this.ownerUserId,
        clientRequestId: intent.clientRequestId,
        idempotencyKey: intent.idempotencyKey,
        state: intent.state,
        createdAt: intent.createdAt,
        updatedAt: intent.updatedAt,
      };
      this.memoryReservations.set(this.ownerUserId, resRecord);
      this.memoryFallback.set(intent.clientRequestId, intent);
    } catch (err) {
      if (err instanceof ActiveReservationConflictError) {
        throw err;
      }
      if (!this.allowMemoryFallback) {
        throw new StorageUnavailableError(
          err instanceof Error ? err.message : "IndexedDB reservation failed"
        );
      }
      // Memory fallback mode when allowed
      const existing = this.memoryReservations.get(this.ownerUserId);
      if (
        existing &&
        existing.clientRequestId !== intent.clientRequestId &&
        !isTerminalState(existing.state)
      ) {
        throw new ActiveReservationConflictError(
          "Another payment reservation is already active for this account.",
          existing
        );
      }
      const resRecord: ActiveReservation = {
        ownerUserId: this.ownerUserId,
        clientRequestId: intent.clientRequestId,
        idempotencyKey: intent.idempotencyKey,
        state: intent.state,
        createdAt: intent.createdAt,
        updatedAt: intent.updatedAt,
      };
      this.memoryReservations.set(this.ownerUserId, resRecord);
      this.memoryFallback.set(intent.clientRequestId, {
        ...intent,
        persistenceFailed: true,
      });
    }

    this.broadcastChange();
  }

  /**
   * Resolves an intent to a terminal state (COMPLETED, FAILED, or REVERSED).
   * Updates the intent record and removes the active reservation.
   */
  async resolveTerminal(
    clientRequestId: string,
    outcomeState: "RESOLVED_COMPLETED" | "RESOLVED_FAILED" | "RESOLVED_REVERSED",
    paymentId?: string
  ): Promise<void> {
    const now = new Date().toISOString();
    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction(
          [ACTIVE_RESERVATIONS_STORE_NAME, UNRESOLVED_STORE_NAME],
          "readwrite"
        );
        const activeStore = tx.objectStore(ACTIVE_RESERVATIONS_STORE_NAME);
        const intentStore = tx.objectStore(UNRESOLVED_STORE_NAME);

        const getReq = intentStore.get(clientRequestId);
        getReq.onerror = () => reject(getReq.error ?? new Error("Intent fetch failed"));
        getReq.onsuccess = () => {
          const existing = getReq.result as DurableUnresolvedIntent | undefined;
          if (existing && existing.ownerUserId === this.ownerUserId) {
            const updated: DurableUnresolvedIntent = {
              ...existing,
              state: outcomeState,
              updatedAt: now,
              ...(paymentId ? { paymentId } : {}),
            };
            intentStore.put(updated);
          }

          const resReq = activeStore.get(this.ownerUserId);
          resReq.onsuccess = () => {
            const activeRes = resReq.result as ActiveReservation | undefined;
            if (activeRes && activeRes.clientRequestId === clientRequestId) {
              activeStore.delete(this.ownerUserId);
            }
          };
        };

        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error ?? new Error("Terminal resolution transaction failed"));
        tx.onabort = () => reject(tx.error ?? new Error("Terminal resolution transaction aborted"));
      });
    } catch {
      // Memory fallback if DB unavailable
    }

    const memExisting = this.memoryFallback.get(clientRequestId);
    if (memExisting && memExisting.ownerUserId === this.ownerUserId) {
      this.memoryFallback.set(clientRequestId, {
        ...memExisting,
        state: outcomeState,
        updatedAt: now,
        ...(paymentId ? { paymentId } : {}),
      });
    }
    const memRes = this.memoryReservations.get(this.ownerUserId);
    if (memRes && memRes.clientRequestId === clientRequestId) {
      this.memoryReservations.delete(this.ownerUserId);
    }

    this.broadcastChange();
  }

  /**
   * Updates non-terminal progress (e.g. PROCESSING, UNCERTAIN) without releasing the active reservation.
   */
  async updateProgress(
    clientRequestId: string,
    state: UnresolvedState,
    paymentId?: string,
    lastHttpStatus?: number,
    lastError?: string
  ): Promise<void> {
    const now = new Date().toISOString();
    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction(
          [ACTIVE_RESERVATIONS_STORE_NAME, UNRESOLVED_STORE_NAME],
          "readwrite"
        );
        const activeStore = tx.objectStore(ACTIVE_RESERVATIONS_STORE_NAME);
        const intentStore = tx.objectStore(UNRESOLVED_STORE_NAME);

        const getReq = intentStore.get(clientRequestId);
        getReq.onerror = () => reject(getReq.error ?? new Error("Intent fetch failed"));
        getReq.onsuccess = () => {
          const existing = getReq.result as DurableUnresolvedIntent | undefined;
          if (existing && existing.ownerUserId === this.ownerUserId) {
            const updated: DurableUnresolvedIntent = {
              ...existing,
              state,
              updatedAt: now,
              ...(paymentId ? { paymentId } : {}),
              ...(lastHttpStatus !== undefined ? { lastHttpStatus } : {}),
              ...(lastError !== undefined ? { lastError } : {}),
            };
            intentStore.put(updated);
          }

          const resReq = activeStore.get(this.ownerUserId);
          resReq.onsuccess = () => {
            const activeRes = resReq.result as ActiveReservation | undefined;
            if (activeRes && activeRes.clientRequestId === clientRequestId) {
              const updatedRes: ActiveReservation = {
                ...activeRes,
                state,
                updatedAt: now,
              };
              activeStore.put(updatedRes);
            }
          };
        };

        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error ?? new Error("Progress update transaction failed"));
        tx.onabort = () => reject(tx.error ?? new Error("Progress update transaction aborted"));
      });
    } catch {
      // Memory fallback
    }

    const memExisting = this.memoryFallback.get(clientRequestId);
    if (memExisting && memExisting.ownerUserId === this.ownerUserId) {
      this.memoryFallback.set(clientRequestId, {
        ...memExisting,
        state,
        updatedAt: now,
        ...(paymentId ? { paymentId } : {}),
        ...(lastHttpStatus !== undefined ? { lastHttpStatus } : {}),
        ...(lastError !== undefined ? { lastError } : {}),
      });
    }
    const memRes = this.memoryReservations.get(this.ownerUserId);
    if (memRes && memRes.clientRequestId === clientRequestId) {
      this.memoryReservations.set(this.ownerUserId, {
        ...memRes,
        state,
        updatedAt: now,
      });
    }

    this.broadcastChange();
  }

  /**
   * Backward-compatible save method (updates or persists intent).
   */
  async save(intent: DurableUnresolvedIntent): Promise<DurableUnresolvedIntent> {
    if (intent.ownerUserId !== this.ownerUserId) {
      throw new Error("Tenant isolation violation: intent does not belong to store owner");
    }
    let persistedIntent = intent;
    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction(
          [ACTIVE_RESERVATIONS_STORE_NAME, UNRESOLVED_STORE_NAME],
          "readwrite"
        );
        const intentStore = tx.objectStore(UNRESOLVED_STORE_NAME);
        const activeStore = tx.objectStore(ACTIVE_RESERVATIONS_STORE_NAME);

        intentStore.put(intent);
        if (!isTerminalState(intent.state)) {
          const res: ActiveReservation = {
            ownerUserId: this.ownerUserId,
            clientRequestId: intent.clientRequestId,
            idempotencyKey: intent.idempotencyKey,
            state: intent.state,
            createdAt: intent.createdAt,
            updatedAt: intent.updatedAt,
          };
          activeStore.put(res);
        } else {
          const getReq = activeStore.get(this.ownerUserId);
          getReq.onsuccess = () => {
            const activeRes = getReq.result as ActiveReservation | undefined;
            if (activeRes && activeRes.clientRequestId === intent.clientRequestId) {
              activeStore.delete(this.ownerUserId);
            }
          };
        }

        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error ?? new Error("Transaction failed"));
        tx.onabort = () => reject(tx.error ?? new Error("Transaction aborted"));
      });
      this.memoryFallback.set(intent.clientRequestId, intent);
      if (!isTerminalState(intent.state)) {
        this.memoryReservations.set(this.ownerUserId, {
          ownerUserId: this.ownerUserId,
          clientRequestId: intent.clientRequestId,
          idempotencyKey: intent.idempotencyKey,
          state: intent.state,
          createdAt: intent.createdAt,
          updatedAt: intent.updatedAt,
        });
      } else {
        const existingMem = this.memoryReservations.get(this.ownerUserId);
        if (existingMem && existingMem.clientRequestId === intent.clientRequestId) {
          this.memoryReservations.delete(this.ownerUserId);
        }
      }
    } catch {
      this.memoryOnlyMode = true;
      persistedIntent = { ...intent, persistenceFailed: true };
      this.memoryFallback.set(intent.clientRequestId, persistedIntent);
      if (!isTerminalState(intent.state)) {
        this.memoryReservations.set(this.ownerUserId, {
          ownerUserId: this.ownerUserId,
          clientRequestId: intent.clientRequestId,
          idempotencyKey: intent.idempotencyKey,
          state: intent.state,
          createdAt: intent.createdAt,
          updatedAt: intent.updatedAt,
        });
      } else {
        this.memoryReservations.delete(this.ownerUserId);
      }
    }
    this.broadcastChange();
    return persistedIntent;
  }

  async getActiveReservation(): Promise<ActiveReservation | null> {
    try {
      const db = await this.open();
      const res = await new Promise<ActiveReservation | undefined>((resolve, reject) => {
        const tx = db.transaction(ACTIVE_RESERVATIONS_STORE_NAME, "readonly");
        const store = tx.objectStore(ACTIVE_RESERVATIONS_STORE_NAME);
        const req = store.get(this.ownerUserId);
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error ?? new Error("Active reservation read failed"));
      });
      if (res && res.ownerUserId === this.ownerUserId && !isTerminalState(res.state)) {
        return res;
      }
    } catch {
      const res = this.memoryReservations.get(this.ownerUserId);
      if (res && res.ownerUserId === this.ownerUserId && !isTerminalState(res.state)) {
        return res;
      }
    }
    return null;
  }

  async getActive(): Promise<DurableUnresolvedIntent | null> {
    try {
      const activeRes = await this.getActiveReservation();
      if (activeRes) {
        const intent = await this.get(activeRes.clientRequestId);
        if (intent && !isTerminalState(intent.state)) {
          return intent;
        }
      }

      // Fallback scan of unresolved-intents
      const db = await this.open();
      const records = await new Promise<DurableUnresolvedIntent[]>((resolve, reject) => {
        const tx = db.transaction(UNRESOLVED_STORE_NAME, "readonly");
        const store = tx.objectStore(UNRESOLVED_STORE_NAME);
        const request = store.index("ownerUserId").getAll(this.ownerUserId);
        request.onsuccess = () => resolve(request.result ?? []);
        request.onerror = () => reject(request.error ?? new Error("Read failed"));
      });
      const nonTerminal = records
        .filter((r) => r.ownerUserId === this.ownerUserId && !isTerminalState(r.state))
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
      if (nonTerminal.length > 0) {
        return nonTerminal[0];
      }
    } catch {
      const memRes = this.memoryReservations.get(this.ownerUserId);
      if (memRes && !isTerminalState(memRes.state)) {
        const intent = this.memoryFallback.get(memRes.clientRequestId);
        if (intent && intent.ownerUserId === this.ownerUserId && !isTerminalState(intent.state)) {
          return intent;
        }
      }

      const memoryRecords = Array.from(this.memoryFallback.values())
        .filter((r) => r.ownerUserId === this.ownerUserId && !isTerminalState(r.state))
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

  async markResolved(
    clientRequestId: string,
    state: "RESOLVED_COMPLETED" | "RESOLVED_FAILED" | "RESOLVED_REVERSED",
    paymentId?: string
  ): Promise<void> {
    await this.resolveTerminal(clientRequestId, state, paymentId);
  }

  async clearResolved(clientRequestId: string): Promise<void> {
    const existing = await this.get(clientRequestId);
    // Never delete a non-terminal intent!
    if (!existing || !isTerminalState(existing.state)) return;

    try {
      const db = await this.open();
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction(
          [ACTIVE_RESERVATIONS_STORE_NAME, UNRESOLVED_STORE_NAME],
          "readwrite"
        );
        const intentStore = tx.objectStore(UNRESOLVED_STORE_NAME);
        const activeStore = tx.objectStore(ACTIVE_RESERVATIONS_STORE_NAME);

        intentStore.delete(clientRequestId);
        const req = activeStore.get(this.ownerUserId);
        req.onsuccess = () => {
          const res = req.result as ActiveReservation | undefined;
          if (res && res.clientRequestId === clientRequestId) {
            activeStore.delete(this.ownerUserId);
          }
        };

        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error);
      });
      this.memoryFallback.delete(clientRequestId);
      const memRes = this.memoryReservations.get(this.ownerUserId);
      if (memRes && memRes.clientRequestId === clientRequestId) {
        this.memoryReservations.delete(this.ownerUserId);
      }
    } catch {
      this.memoryFallback.delete(clientRequestId);
      const memRes = this.memoryReservations.get(this.ownerUserId);
      if (memRes && memRes.clientRequestId === clientRequestId) {
        this.memoryReservations.delete(this.ownerUserId);
      }
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
