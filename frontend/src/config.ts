/** The API has one origin for JSON requests and the operational event stream. */
export function resolveApiBaseUrl(value: unknown, production = false, pageOrigin?: string): string {
  if (value == null || value === "") {
    if (!production) return "http://localhost:8080";
    if (!pageOrigin) throw new Error("A production API origin is required.");
    value = pageOrigin;
  }
  if (typeof value !== "string") throw new Error("The API origin must be a URL.");
  const candidate = value.trim();
  let parsed: URL;
  try {
    parsed = new URL(candidate);
  } catch {
    throw new Error("The API origin must be an absolute URL.");
  }
  if (!(["http:", "https:"].includes(parsed.protocol)) || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
    throw new Error("The API origin must be a plain HTTP(S) origin without credentials or a path.");
  }
  if (production && parsed.protocol !== "https:") throw new Error("The production API origin must use HTTPS.");
  return parsed.origin;
}

const viteEnv = (import.meta as ImportMeta & { env?: { VITE_API_BASE_URL?: string; PROD?: boolean } }).env;
export const apiBaseUrl = resolveApiBaseUrl(
  viteEnv?.VITE_API_BASE_URL,
  viteEnv?.PROD ?? false,
  typeof window === "undefined" ? undefined : window.location.origin,
);
