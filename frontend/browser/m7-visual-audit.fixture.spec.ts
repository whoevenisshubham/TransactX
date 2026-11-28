import { test, expect } from "@playwright/test";
import { installFixtureApi, paymentId, investigationId, fulfillFixture, fixture, worklistFixture } from "./support/opsFixtures";
import { adminFixtures, participant, operationId, start, end } from "./support/adminFixtures";

const widths = [1440, 1024, 768, 390, 320];

for (const theme of ["light", "dark"] as const) {
  for (const width of widths) {
    const height = width > 960 ? 900 : width === 768 ? 1024 : 844;

    test(`M7 visual capture: Public landing & Auth in ${theme} at ${width}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });

      // Landing
      await page.goto("/");
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-landing-${theme}-${width}.png`), fullPage: true });

      // Auth
      await page.goto("/login");
      await expect(page.getByRole("heading", { name: "Sign in to continue" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-auth-${theme}-${width}.png`), fullPage: true });
    });

    test(`M7 visual capture: Customer Dashboard & Send in ${theme} at ${width}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });

      await page.addInitScript(() => sessionStorage.setItem("transactx.token", "m7-customer-token"));
      await page.route("http://localhost:8080/**", async (route) => {
        const url = new URL(route.request().url());
        const headers = { "access-control-allow-origin": "http://127.0.0.1:4173", "access-control-allow-headers": "Authorization, Content-Type" };
        if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });

        if (url.pathname === "/api/me") {
          return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "m7", data: { id: "cust-m7", name: "Alice Explorer", role: "CUSTOMER", paymentIdentifier: "alice@transactx" } }) });
        }
        if (url.pathname === "/api/accounts") {
          return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "m7", data: [{ id: "acc-m7", accountNumber: "TX-1001", balancePaise: 754000, status: "ACTIVE", bankName: "Bank A", bankCode: "BANK-A" }] }) });
        }
        if (url.pathname.startsWith("/api/recipients/")) {
          return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "m7", data: { name: "Studio Store", paymentIdentifier: "store@transactx", status: "ACTIVE" } }) });
        }
        if (url.pathname.startsWith("/api/payments")) {
          return route.fulfill({
            headers,
            contentType: "application/json",
            body: JSON.stringify({
              requestId: "m7",
              data: [
                { id: "11111111-1111-4111-8111-111111111111", amountPaise: 12500, currency: "INR", state: "COMPLETED", senderName: "Alice Explorer", senderPaymentIdentifier: "alice@transactx", receiverName: "Studio Store", receiverPaymentIdentifier: "store@transactx", direction: "SENT", sourceBankCode: "BANK-A", destinationBankCode: "BANK-B", createdAt: new Date().toISOString() },
                { id: "22222222-2222-4222-8222-222222222222", amountPaise: 45000, currency: "INR", state: "PENDING_RECONCILIATION", senderName: "Studio Store", senderPaymentIdentifier: "store@transactx", receiverName: "Alice Explorer", receiverPaymentIdentifier: "alice@transactx", direction: "RECEIVED", sourceBankCode: "BANK-B", destinationBankCode: "BANK-A", createdAt: new Date(Date.now() - 3600000).toISOString() },
              ],
            }),
          });
        }
        return route.fallback();
      });

      // Customer Dashboard
      await page.goto("/");
      await expect(page.getByRole("heading", { name: "Good day, Alice" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-customer-dashboard-${theme}-${width}.png`), fullPage: true });

      // Customer Send Money
      await page.goto("/pay");
      await expect(page.getByRole("heading", { name: "Send money" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-customer-send-${theme}-${width}.png`), fullPage: true });
    });

    test(`M7 visual capture: Merchant Receive & Incoming in ${theme} at ${width}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });

      await page.addInitScript(() => sessionStorage.setItem("transactx.token", "m7-merchant-token"));
      await page.route("http://localhost:8080/**", async (route) => {
        const url = new URL(route.request().url());
        const headers = { "access-control-allow-origin": "http://127.0.0.1:4173", "access-control-allow-headers": "Authorization, Content-Type" };
        if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });

        if (url.pathname === "/api/me") {
          return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "m7", data: { id: "merch-m7", name: "Metro Retail", role: "MERCHANT", paymentIdentifier: "metro@transactx" } }) });
        }
        if (url.pathname === "/api/accounts") {
          return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "m7", data: [{ id: "acc-m7", accountNumber: "TX-M1001", balancePaise: 890000, status: "ACTIVE", bankName: "Bank B", bankCode: "BANK-B" }] }) });
        }
        if (url.pathname === "/api/merchant/receive-info") {
          return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "m7", data: { paymentIdentifier: "metro@transactx", accountNumber: "TX-M1001", accountStatus: "ACTIVE" } }) });
        }
        if (url.pathname.startsWith("/api/payments")) {
          return route.fulfill({
            headers,
            contentType: "application/json",
            body: JSON.stringify({
              requestId: "m7",
              data: [
                { id: "33333333-3333-4333-8333-333333333333", amountPaise: 25000, currency: "INR", state: "COMPLETED", senderName: "Customer Bob", senderPaymentIdentifier: "bob@transactx", receiverName: "Metro Retail", receiverPaymentIdentifier: "metro@transactx", direction: "RECEIVED", sourceBankCode: "BANK-A", destinationBankCode: "BANK-B", createdAt: new Date().toISOString() },
              ],
            }),
          });
        }
        return route.fallback();
      });

      // Merchant Receive
      await page.goto("/merchant/receive");
      await expect(page.getByRole("heading", { name: "Accept payments" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-merchant-receive-${theme}-${width}.png`), fullPage: true });

      // Merchant Incoming
      await page.goto("/merchant/incoming");
      await expect(page.getByRole("heading", { name: "Payments received" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-merchant-incoming-${theme}-${width}.png`), fullPage: true });
    });

    test(`M7 visual capture: Operations Worklist & Investigation in ${theme} at ${width}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });

      await installFixtureApi(page, "OPS_VIEWER");

      // Worklist
      await page.goto("/console/worklist");
      await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-ops-worklist-${theme}-${width}.png`), fullPage: true });

      // Investigation
      await page.goto(`/console/payments/${paymentId}`);
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-ops-investigation-${theme}-${width}.png`), fullPage: true });
    });

    test(`M7 visual capture: Admin Overview, Routing & Integrity in ${theme} at ${width}`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });

      await adminFixtures(page, () => {});

      // Admin Overview
      await page.goto("/console");
      await expect(page.getByRole("heading", { name: "Network overview" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-admin-overview-${theme}-${width}.png`), fullPage: true });

      // Admin Routing
      await page.goto("/console/routing");
      await expect(page.getByRole("heading", { name: "Target Circuit Controls" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-admin-routing-${theme}-${width}.png`), fullPage: true });

      // Admin Integrity
      await page.goto("/console/integrity");
      await expect(page.getByRole("heading", { name: "Ledger Integrity Engine" })).toBeVisible();
      await page.screenshot({ path: testInfo.outputPath(`m7-admin-integrity-${theme}-${width}.png`), fullPage: true });
    });
  }
}
