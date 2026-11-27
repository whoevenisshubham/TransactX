import { ApiError, apiRequest } from "../api";

export type DemoPersona = "customer" | "recipient" | "merchant" | "operator";
export type AuthOptions = { demoEnabled: boolean; registrationEnabled: boolean; googleEnabled: boolean };
export const authClient = {
  options: (signal: AbortSignal) => apiRequest<AuthOptions>("/api/auth/options", { signal }),
  async demo(persona: DemoPersona, callerSignal?: AbortSignal) {
    const timeoutSignal = AbortSignal.timeout(60_000);
    const combinedController = new AbortController();
    const onTimeout = () => combinedController.abort();
    const onCallerAbort = () => combinedController.abort();
    timeoutSignal.addEventListener("abort", onTimeout);
    callerSignal?.addEventListener("abort", onCallerAbort);
    try {
      return await apiRequest<{ token: string }>("/api/auth/demo", {
        method: "POST",
        body: JSON.stringify({ persona }),
        signal: combinedController.signal,
      });
    } catch (error) {
      if (timeoutSignal.aborted && !callerSignal?.aborted) {
        throw new ApiError(
          "Sign-in took longer than expected. The service may be starting or unreachable. Please try again.",
          "SIGN_IN_TIMEOUT",
          0
        );
      }
      throw error;
    } finally {
      timeoutSignal.removeEventListener("abort", onTimeout);
      callerSignal?.removeEventListener("abort", onCallerAbort);
    }
  },
};
