import { ApiError, apiRequest } from "../api";

export type DemoPersona = "customer" | "recipient" | "merchant" | "operator";
export type AuthOptions = { demoEnabled: boolean; registrationEnabled: boolean; googleEnabled: boolean };
export const authClient = {
  options: (signal: AbortSignal) => apiRequest<AuthOptions>("/api/auth/options", { signal }),
  async demo(persona: DemoPersona) {
    const signal = AbortSignal.timeout(60_000);
    try {
      return await apiRequest<{ token: string }>("/api/auth/demo", { method: "POST", body: JSON.stringify({ persona }), signal });
    } catch (error) {
      if (signal.aborted) throw new ApiError("Sign-in took longer than expected. The service may be starting or unreachable. Please try again.", "SIGN_IN_TIMEOUT", 0);
      throw error;
    }
  },
};
