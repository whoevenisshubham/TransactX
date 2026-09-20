import type { ApiError } from "./api";
import { OFFLINE_REPLAY_LEASE_LOST, type OfflineIntent, type OfflineIntentQueue, type ReplayMetadata } from "./offlineQueue";
import type { Payment } from "./types";

export type ReplayClient = {
  createPayment: (payload: OfflineIntent["payload"], idempotencyKey: string, clientRequestId: string) => Promise<{ payment: Payment; status: number }>;
  getPayment: (paymentID: string) => Promise<Payment>;
};

export type ReplayResult = {
  clientRequestId: string;
  state: OfflineIntent["state"];
  outcome: "SYNCED" | "PENDING" | "RETRYABLE" | "FAILED";
};

const MAX_RETRY_DELAY_MS = 30 * 60 * 1000;
const BASE_RETRY_DELAY_MS = 30 * 1000;

export function retryDelayMs(attemptCount: number): number {
  const exponent = Math.max(0, Math.min(attemptCount - 1, 10));
  return Math.min(BASE_RETRY_DELAY_MS * (2 ** exponent), MAX_RETRY_DELAY_MS);
}

function paymentIsFinal(payment: Payment): boolean {
  return payment.state === "COMPLETED" || payment.state === "FAILED" || payment.state === "REVERSED";
}

function paymentIsSuccessful(payment: Payment): boolean {
  return payment.state === "COMPLETED";
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : "Payment replay failed";
}

function isRetryableError(error: unknown): boolean {
  const candidate = error as Partial<ApiError>;
  return candidate.status === 0 || candidate.status === 408 || candidate.status === 429 || (typeof candidate.status === "number" && candidate.status >= 500) || candidate.code === "BANK_UNAVAILABLE" || candidate.code === "NETWORK_ERROR";
}

export class OfflineReplayWorker {
  private running: Promise<ReplayResult[]> | null = null;
  private stopped = false;

  constructor(private readonly queue: OfflineIntentQueue, private readonly client: ReplayClient, private readonly owner = `replay-worker-${crypto.randomUUID()}`, private readonly leaseMilliseconds = 30_000) {}

  stop(): void {
    this.stopped = true;
  }

  run(now = new Date()): Promise<ReplayResult[]> {
    if (!this.running) this.running = this.replayAll(now).finally(() => { this.running = null; });
    return this.running;
  }

  private async replayAll(now: Date): Promise<ReplayResult[]> {
    const results: ReplayResult[] = [];
    while (true) {
      if (this.stopped) return results;
      const intent = await this.queue.claimEligible(this.owner, now, this.leaseMilliseconds);
      if (!intent) return results;
      results.push(await this.replayOne(intent, now));
    }
  }

  private async replayOne(intent: OfflineIntent, now: Date): Promise<ReplayResult> {
    if (intent.replay?.paymentId) {
      try {
        const payment = await this.withLeaseHeartbeat(intent, () => this.client.getPayment(intent.replay!.paymentId!));
        return this.persistPayment(intent, payment, 200, now);
      } catch (error) {
        return this.handleReplayError(intent, error, now);
      }
    }

    try {
      const response = await this.withLeaseHeartbeat(intent, () => this.client.createPayment(intent.payload, intent.idempotencyKey, intent.clientRequestId));
      return this.persistPayment(intent, response.payment, response.status, now);
    } catch (error) {
      return this.handleReplayError(intent, error, now);
    }
  }

  private async handleReplayError(intent: OfflineIntent, error: unknown, now: Date): Promise<ReplayResult> {
    if (isLeaseLost(error)) return { clientRequestId: intent.clientRequestId, state: "SYNCING", outcome: "PENDING" };
    try {
      return await this.persistFailureOrRetry(intent, error, now);
    } catch (persistenceError) {
      if (isLeaseLost(persistenceError)) return { clientRequestId: intent.clientRequestId, state: "SYNCING", outcome: "PENDING" };
      throw persistenceError;
    }
  }

  private async persistPayment(intent: OfflineIntent, payment: Payment, status: number, now: Date): Promise<ReplayResult> {
    const metadata: ReplayMetadata = { paymentId: payment.id, paymentState: payment.state, lastHttpStatus: status };
    if (paymentIsSuccessful(payment) && status !== 202) {
      const updated = await this.queue.recordSynced(intent.clientRequestId, metadata, now, this.owner);
      return { clientRequestId: updated.clientRequestId, state: updated.state, outcome: "SYNCED" };
    }
    if (paymentIsFinal(payment) && !paymentIsSuccessful(payment)) {
      const updated = await this.queue.recordFailure(intent.clientRequestId, payment.failureReason ?? "The authoritative payment was not completed", metadata, now, this.owner);
      return { clientRequestId: updated.clientRequestId, state: updated.state, outcome: "FAILED" };
    }
    const updated = await this.queue.recordPending(intent.clientRequestId, metadata, undefined, new Date(now.getTime() + retryDelayMs(intent.retry.attemptCount + 1)), now, this.owner);
    return { clientRequestId: updated.clientRequestId, state: updated.state, outcome: "PENDING" };
  }

  private async persistFailureOrRetry(intent: OfflineIntent, error: unknown, now: Date): Promise<ReplayResult> {
    const message = errorMessage(error);
    if (isRetryableError(error)) {
      const updated = error instanceof Error && (error as Partial<ApiError>).status === 0
        ? await this.queue.recordPending(intent.clientRequestId, {}, message, new Date(now.getTime() + retryDelayMs(intent.retry.attemptCount + 1)), now, this.owner)
        : await this.queue.recordRetry(intent.clientRequestId, message, new Date(now.getTime() + retryDelayMs(intent.retry.attemptCount + 1)), now, {}, this.owner);
      return { clientRequestId: updated.clientRequestId, state: updated.state, outcome: error instanceof Error && (error as Partial<ApiError>).status === 0 ? "PENDING" : "RETRYABLE" };
    }
    const updated = await this.queue.recordFailure(intent.clientRequestId, message, {}, now, this.owner);
    return { clientRequestId: updated.clientRequestId, state: updated.state, outcome: "FAILED" };
  }

  private async withLeaseHeartbeat<T>(intent: OfflineIntent, operation: () => Promise<T>): Promise<T> {
    const intervalMilliseconds = Math.max(1, Math.floor(this.leaseMilliseconds / 3));
    let lost = false;
    const heartbeat = setInterval(() => {
      void this.queue.renewLease(intent.clientRequestId, this.owner, new Date(), this.leaseMilliseconds).then((renewed) => { if (!renewed) lost = true; }).catch(() => { lost = true; });
    }, intervalMilliseconds);
    try {
      const result = await operation();
      if (this.stopped || lost) throw new Error("offline replay lease was lost");
      return result;
    } finally {
      clearInterval(heartbeat);
    }
  }
}

function isLeaseLost(error: unknown): boolean {
  return error instanceof Error && error.message === OFFLINE_REPLAY_LEASE_LOST;
}