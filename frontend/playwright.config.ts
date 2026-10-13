import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./browser",
  testMatch: "*.fixture.spec.ts",
  testIgnore: "offline-shell.fixture.spec.ts",
  fullyParallel: false,
  reporter: "list",
  timeout: 30000,
  use: {
    baseURL: "http://127.0.0.1:4173",
    browserName: "chromium",
    channel: process.env.CI ? undefined : "msedge",
    headless: true,
    trace: "retain-on-failure",
  },
  webServer: {
    command: "npm run dev -- --host 127.0.0.1 --port 4173 --strictPort",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: false,
    timeout: 30000,
  },
});
