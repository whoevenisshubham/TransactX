import { defineConfig } from "@playwright/test";

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
  },
});
