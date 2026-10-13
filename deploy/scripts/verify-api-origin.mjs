import { isIP } from "node:net";

const allowMissing = process.argv.slice(2).includes("--allow-missing");
if (process.argv.slice(2).some((arg) => arg !== "--allow-missing")) {
  process.stderr.write("Unknown API origin validation option.\n");
  process.exit(2);
}

const raw = process.env.VITE_API_BASE_URL;
if (!raw && allowMissing) process.exit(0);

let url;
try {
  if (!raw || raw !== raw.trim()) throw new Error("missing or padded");
  url = new URL(raw);
} catch {
  process.stderr.write("VITE_API_BASE_URL must be an actual public HTTPS API origin.\n");
  process.exit(1);
}

const hostname = url.hostname.toLowerCase();
const reserved = hostname === "localhost" || hostname.endsWith(".localhost") ||
  ["example.com", "example.org", "example.net"].some((name) => hostname === name || hostname.endsWith(`.${name}`)) ||
  [".example", ".invalid", ".test"].some((suffix) => hostname.endsWith(suffix));
if (url.protocol !== "https:" || url.username || url.password || url.pathname !== "/" ||
    url.search || url.hash || !hostname.includes(".") || isIP(hostname) || reserved) {
  process.stderr.write("VITE_API_BASE_URL must be an actual public HTTPS API origin without credentials, path, query or fragment.\n");
  process.exit(1);
}

process.stdout.write(`${url.origin}\n`);
