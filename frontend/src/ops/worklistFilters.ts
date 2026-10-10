import type { WorklistFilters } from "./contracts";

export const paymentStates = [
  "CREATED", "VALIDATING", "ROUTING", "PROCESSING", "PENDING_RECONCILIATION",
  "BANK_SETTLED_CENTRAL_PENDING", "COMMITTED", "COMPLETED", "FAILED", "REVERSED", "REPLAY_FAILED",
] as const;
export const recoveryStatuses = ["none", "scheduled", "running", "deferred", "needs_attention", "resolved"] as const;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function parseWorklistFilters(search: string): { filters: WorklistFilters; warning: string | null } {
  const params = new URLSearchParams(search);
  const filters: WorklistFilters = {};
  let warning: string | null = null;
  const state = params.get("state");
  if (state) {
    const states = state.split(",");
    if (states.length <= 4 && states.every((item) => paymentStates.includes(item as typeof paymentStates[number]))) filters.state = state;
    else warning = "An invalid payment state filter was removed.";
  }
  const recoveryStatus = params.get("recoveryStatus");
  if (recoveryStatus) {
    if (recoveryStatuses.includes(recoveryStatus as typeof recoveryStatuses[number])) filters.recoveryStatus = recoveryStatus as WorklistFilters["recoveryStatus"];
    else warning = "An invalid recovery filter was removed.";
  }
  const bankCode = params.get("bankCode");
  if (bankCode) {
    if (/^[A-Z0-9-]{2,20}$/.test(bankCode)) filters.bankCode = bankCode;
    else warning = "An invalid bank filter was removed.";
  }
  const paymentId = params.get("paymentId");
  if (paymentId) {
    if (uuid.test(paymentId)) filters.paymentId = paymentId.toLowerCase();
    else warning = "Search requires an exact payment UUID.";
  }
  for (const name of ["createdFrom", "createdTo"] as const) {
    const value = params.get(name);
    if (!value) continue;
    if (!Number.isNaN(Date.parse(value)) && value.length <= 40) filters[name] = value;
    else warning = "An invalid time filter was removed.";
  }
  if (filters.createdFrom && filters.createdTo && Date.parse(filters.createdFrom) > Date.parse(filters.createdTo)) {
    delete filters.createdTo;
    warning = "The end time was earlier than the start time and was removed.";
  }
  const cursor = params.get("cursor");
  if (cursor && cursor.length <= 1024) filters.cursor = cursor;
  const limit = params.get("limit");
  if (limit) {
    const parsed = Number(limit);
    if (Number.isInteger(parsed) && parsed >= 1 && parsed <= 100) filters.limit = parsed;
    else warning = "An invalid page size was removed.";
  }
  return { filters, warning };
}

export function worklistSearch(filters: WorklistFilters): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) if (value !== undefined && value !== "") params.set(key, String(value));
  return params.size ? `?${params}` : "";
}

export function isoToLocalInput(iso: string | undefined): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
  return local.toISOString().slice(0, 16);
}

export function localInputToIso(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}
