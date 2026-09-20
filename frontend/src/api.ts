import type { Account, Payment, Recipient, User } from "./types";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

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

export async function apiRequest<T>(path: string, options: RequestInit = {}, token?: string): Promise<T> {
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
    throw new ApiError("We couldn't reach TransactX. Check your connection and try again.", "NETWORK_ERROR", 0);
  }

  const body = (await response.json().catch(() => ({}))) as ApiResponse<T> | ErrorResponse;
  if (!response.ok) {
    const error = body as ErrorResponse;
    throw new ApiError(error.error?.message ?? "The request could not be completed.", error.error?.code, response.status);
  }
  return (body as ApiResponse<T>).data;
}

export const api = {
  login: (identifier: string, password: string) => apiRequest<{ token: string; user: User }>("/api/auth/login", { method: "POST", body: JSON.stringify({ identifier, password }) }),
  register: (form: Record<string, string>) => apiRequest<User>("/api/auth/register", { method: "POST", body: JSON.stringify(form) }),
  me: (token: string) => apiRequest<User>("/api/me", {}, token),
  accounts: (token: string) => apiRequest<Account[]>("/api/accounts", {}, token),
  resolveRecipient: (identifier: string, token: string) => apiRequest<Recipient>(`/api/recipients/${encodeURIComponent(identifier)}`, {}, token),
  payments: (token: string) => apiRequest<Payment[]>("/api/payments?limit=50", {}, token),
  payment: (id: string, token: string) => apiRequest<Payment>(`/api/payments/${encodeURIComponent(id)}`, {}, token),
  createPayment: (input: { recipient: string; amountPaise: number; currency: string; note?: string }, token: string, idempotencyKey: string) => apiRequest<Payment>("/api/payments", { method: "POST", headers: { "Idempotency-Key": idempotencyKey }, body: JSON.stringify(input) }, token),
};
