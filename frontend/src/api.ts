import type {
  Account,
  ChaosScenario,
  CircuitTargetSnapshot,
  CircuitTransitionEvent,
  HealthSample,
  HealthSnapshot,
  MerchantReceiveInfo,
  Payment,
  Recipient,
  User,
} from "./types";

const apiBaseUrl = (typeof import.meta !== "undefined" && (import.meta as any)?.env?.VITE_API_BASE_URL) || "http://localhost:8080";

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(message: string, code = "REQUEST_FAILED", status = 500) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

type ApiResponse<T> = { requestId: string; data: T };
type ErrorResponse = { requestId?: string; error?: { code?: string; message?: string } };

function safeErrorMessage(code: string | undefined, fallback: string): string {
  switch (code) {
    case "INVALID_CREDENTIALS":
      return "Your username or password is incorrect.";
    case "INVALID_REQUEST":
      return "Please check your details and try again.";
    case "USER_ALREADY_EXISTS":
      return "An account with that payment ID already exists.";
    case "UNAUTHORIZED":
      return "Please sign in again.";
    case "RECIPIENT_NOT_FOUND":
      return "That payment ID could not be found.";
    case "INSUFFICIENT_FUNDS":
      return "Your balance is too low for this payment.";
    case "BANK_UNAVAILABLE":
      return "Payments are temporarily unavailable. Try again shortly.";
    case "NETWORK_ERROR":
      return "We couldn't reach TransactX. Check your connection and try again.";
    default:
      return fallback;
  }
}

export async function apiRequest<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
  return (await apiRequestWithStatus<T>(path, options, token)).data;
}

export async function apiRequestWithStatus<T>(path: string, options: RequestInit = {}, token?: string): Promise<{ data: T; status: number }> {
  let response: Response;
  try {
    response = await fetch(`${apiBaseUrl}${path}`, {
      ...options,
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
        ...options.headers,
      },
    });
  } catch {
    throw new ApiError(safeErrorMessage("NETWORK_ERROR", "The request could not be completed."), "NETWORK_ERROR", 0);
  }

  const body = (await response.json().catch(() => ({}))) as ApiResponse<T> | ErrorResponse;
  if (!response.ok) {
    const error = body as ErrorResponse;
    const code = error.error?.code ?? "REQUEST_FAILED";
    const message = safeErrorMessage(code, "The request could not be completed.");
    throw new ApiError(message, code, response.status);
  }
  return { data: (body as ApiResponse<T>).data, status: response.status };
}

export const api = {
  login: (identifier: string, password: string) => apiRequest<{ token: string; user: User }>("/api/auth/login", { method: "POST", body: JSON.stringify({ identifier, password }) }),
  register: (form: Record<string, string>) => apiRequest<User>("/api/auth/register", { method: "POST", body: JSON.stringify(form) }),
  me: (token: string) => apiRequest<User>("/api/me", {}, token),
  accounts: (token: string) => apiRequest<Account[]>("/api/accounts", {}, token),
  resolveRecipient: (identifier: string, token: string) => apiRequest<Recipient>(`/api/recipients/${encodeURIComponent(identifier)}`, {}, token),
  payments: (token: string) => apiRequest<Payment[]>("/api/payments?limit=50", {}, token),
  payment: (id: string, token: string) => apiRequest<Payment>(`/api/payments/${encodeURIComponent(id)}`, {}, token),
  createPayment: (input: { recipient: string; amountPaise: number; currency: string; note?: string }, token: string, idempotencyKey: string, clientRequestId?: string) => apiRequest<Payment>("/api/payments", { method: "POST", headers: { "Idempotency-Key": idempotencyKey, ...(clientRequestId ? { "X-Request-ID": clientRequestId } : {}) }, body: JSON.stringify(input) }, token),
  createPaymentWithStatus: async (input: { recipient: string; amountPaise: number; currency: string; note?: string }, token: string, idempotencyKey: string, clientRequestId?: string) => apiRequestWithStatus<Payment>("/api/payments", { method: "POST", headers: { "Idempotency-Key": idempotencyKey, ...(clientRequestId ? { "X-Request-ID": clientRequestId } : {}) }, body: JSON.stringify(input) }, token),
  merchantReceiveInfo: (token: string) => apiRequest<MerchantReceiveInfo>("/api/merchant/receive-info", {}, token),

  // OPS_ADMIN / Network Console APIs (real M2 endpoints)
  opsCircuits: (token: string) => apiRequest<Record<string, CircuitTargetSnapshot>>("/api/ops/circuit", {}, token),
  opsCircuit: (targetId: string, token: string) => apiRequest<CircuitTargetSnapshot>(`/api/ops/circuit/${encodeURIComponent(targetId)}`, {}, token),
  opsCircuitEvents: (targetId: string, token: string) => apiRequest<CircuitTransitionEvent[]>(`/api/ops/circuit/${encodeURIComponent(targetId)}/events`, {}, token),
  opsHealthSnapshot: (targetId: string, token: string) => apiRequest<HealthSnapshot>(`/api/ops/health/${encodeURIComponent(targetId)}`, {}, token),
  opsHealthSample: (targetId: string, token: string) => apiRequest<HealthSample>(`/api/ops/health/${encodeURIComponent(targetId)}/sample`, { method: "POST" }, token),
  opsChaosScenarios: (token: string, activeOnly = false) => apiRequest<ChaosScenario[]>(`/api/ops/chaos/scenarios${activeOnly ? "?active=true" : ""}`, {}, token),
  opsChaosScenario: (scenarioId: string, token: string) => apiRequest<ChaosScenario>(`/api/ops/chaos/scenarios/${encodeURIComponent(scenarioId)}`, {}, token),
  opsChaosStart: (req: { scenarioId: string; type: string; targetId: string; parameters: Record<string, unknown> }, token: string) => apiRequest<ChaosScenario>("/api/ops/chaos/start", { method: "POST", body: JSON.stringify(req) }, token),
  opsChaosStop: (scenarioId: string, token: string) => apiRequest<ChaosScenario>(`/api/ops/chaos/scenarios/${encodeURIComponent(scenarioId)}/stop`, { method: "POST" }, token),
  opsChaosReset: (targetId: string | undefined, token: string) => apiRequest<{ status: string; targetId?: string }>("/api/ops/chaos/reset", { method: "POST", body: JSON.stringify({ targetId: targetId || "" }) }, token),
  opsGlobalHealth: () => apiRequest<{ status: string; service: string }>("/health", {}),
  opsDbHealth: () => apiRequest<{ status: string }>("/health/db", {}),
};

