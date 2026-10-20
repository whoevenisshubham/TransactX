import { apiRequest } from "../api";

export type DemoPersona = "customer" | "recipient" | "merchant" | "operator";
export type AuthOptions = { demoEnabled: boolean; registrationEnabled: boolean; googleEnabled: boolean };
export const authClient = {
  options: (signal: AbortSignal) => apiRequest<AuthOptions>("/api/auth/options", { signal }),
  demo: (persona: DemoPersona) => apiRequest<{ token: string }>("/api/auth/demo", { method: "POST", body: JSON.stringify({ persona }) }),
};
