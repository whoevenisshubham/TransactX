import { defineConfig } from "@playwright/test";

// A generated loopback certificate can be pinned without changing system trust.
// Public deployment tests must use the browser's normal certificate validation.
const localCertificatePin = process.env.TX_LIVE_TLS_SPKI;
if (localCertificatePin) {
  const origins = [process.env.TX_LIVE_API_URL, process.env.TX_LIVE_FRONTEND_URL];
  if (process.env.TX_LIVE_DISPOSABLE_DB !== "1" ||
      !/^[A-Za-z0-9+/]{43}=$/.test(localCertificatePin) ||
      !origins.every((origin) => origin && ["localhost", "127.0.0.1"].includes(new URL(origin).hostname))) {
    throw new Error("TLS public-key pinning is limited to explicitly disposable loopback browser checks.");
  }
}

// Run only against an already started, disposable API/database and frontend.
export default defineConfig({
  testDir: "./browser",
  testMatch: "*.live.spec.ts",
  reporter: "list",
  timeout: 30000,
  use: {
    baseURL: process.env.TX_LIVE_FRONTEND_URL ?? "http://127.0.0.1:4174",
    browserName: "chromium",
    channel: process.env.CI ? undefined : "msedge",
    headless: true,
    launchOptions: {
      args: localCertificatePin ? [`--ignore-certificate-errors-spki-list=${localCertificatePin}`] : [],
    },
  },
});
