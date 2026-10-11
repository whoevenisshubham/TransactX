import { apiRequest } from "../api";
import { decodeAnalyticsPage, decodeEvidence, decodeInvestigation, decodeTrace, decodeWorklist, type AnalyticsWindow, type CursorPage, type EvidenceBundle, type Investigation, type RecoveryAttempt, type Trace, type Worklist, type WorklistFilters } from "./contracts";

export interface OpsReadClient {
  worklist(filters: WorklistFilters, token: string, signal?: AbortSignal): Promise<Worklist>;
  evidence(paymentId: string, token: string, signal?: AbortSignal): Promise<EvidenceBundle>;
}

function query(filters: Record<string, string | number | undefined>): string {
  const params = new URLSearchParams();
  for (const [name, value] of Object.entries(filters)) if (value !== undefined && value !== "") params.set(name, String(value));
  return params.size ? `?${params}` : "";
}

export const liveOpsClient: OpsReadClient & {
  recoveryAttempts(paymentId: string, token: string, cursor?: string, signal?: AbortSignal): Promise<CursorPage<RecoveryAttempt>>;
  exportEvidence(paymentId: string, token: string): Promise<unknown>;
  recheck(paymentId: string, reason: string, idempotencyKey: string, token: string): Promise<{ paymentId: string; scheduledAt: string; status: "scheduled" | "already_terminal" }>;
  createInvestigation(paymentId: string, question: string, idempotencyKey: string, token: string): Promise<{ investigationId: string; status: string; mode: "evidence_only" | "llm" }>;
  investigation(id: string, token: string, signal?: AbortSignal): Promise<Investigation>;
  analytics(token: string, cursor?: string, signal?: AbortSignal): Promise<CursorPage<AnalyticsWindow>>;
  trace(traceId: string, token: string, signal?: AbortSignal): Promise<Trace>;
} = {
  async worklist(filters, token, signal) {
    const raw = await apiRequest<unknown>(`/api/ops/worklist${query(filters)}`, { signal }, token);
    return decodeWorklist(raw);
  },
  async evidence(paymentId, token, signal) {
    const raw = await apiRequest<unknown>(`/api/ops/payments/${encodeURIComponent(paymentId)}/evidence`, { signal }, token);
    return decodeEvidence(raw);
  },
  recoveryAttempts(paymentId, token, cursor, signal) {
    return apiRequest<CursorPage<RecoveryAttempt>>(`/api/ops/payments/${encodeURIComponent(paymentId)}/recovery-attempts${query({ cursor })}`, { signal }, token);
  },
  exportEvidence(paymentId, token) {
    return apiRequest<unknown>(`/api/ops/payments/${encodeURIComponent(paymentId)}/evidence/export`, {}, token);
  },
  recheck(paymentId, reason, idempotencyKey, token) {
    return apiRequest(`/api/ops/payments/${encodeURIComponent(paymentId)}/recovery/recheck`, {
      method: "POST", headers: { "Idempotency-Key": idempotencyKey }, body: JSON.stringify({ reason }),
    }, token);
  },
  createInvestigation(paymentId, question, idempotencyKey, token) {
    return apiRequest("/api/ops/investigations", {
      method: "POST", headers: { "Idempotency-Key": idempotencyKey }, body: JSON.stringify({ paymentId, question }),
    }, token);
  },
  async investigation(id, token, signal) {
    const raw = await apiRequest<unknown>(`/api/ops/investigations/${encodeURIComponent(id)}`, { signal }, token);
    return decodeInvestigation(raw);
  },
  async analytics(token, cursor, signal) {
    const raw = await apiRequest<unknown>(`/api/ops/analytics/windows${query({ cursor })}`, { signal }, token);
    return decodeAnalyticsPage(raw);
  },
  async trace(traceId, token, signal) {
    const raw = await apiRequest<unknown>(`/api/ops/traces/${encodeURIComponent(traceId)}`, { signal }, token);
    return decodeTrace(raw);
  },
};
