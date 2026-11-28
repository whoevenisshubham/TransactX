import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { installFixtureApi, paymentId, investigationId, fulfillFixture, fixture, worklistFixture } from "./support/opsFixtures";
import { adminFixtures, participant, operationId, start, end } from "./support/adminFixtures";

const headers = {
  "access-control-allow-origin": "http://127.0.0.1:4173",
  "access-control-allow-headers": "Content-Type, Authorization, X-Request-ID, Idempotency-Key",
  "access-control-allow-methods": "GET, POST, OPTIONS",
};

test.describe("Milestone 6: Critical Cross-Surface End-to-End User Journeys", () => {
  test("Journey 1: New public visitor (Landing -> Demo gateway -> Customer demo login -> Dashboard -> Logout)", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.route("http://localhost:8080/**", async (route) => {
      const url = new URL(route.request().url());
      if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });

      if (url.pathname === "/api/auth/options") {
        return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "j1", data: { demoEnabled: true, registrationEnabled: true, googleEnabled: false } }) });
      }
      if (url.pathname === "/api/auth/demo") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j1",
            data: {
              token: "fixture-customer-token",
              user: { id: "visitor-1", name: "Alice Explorer", role: "CUSTOMER", paymentIdentifier: "alice@transactx" },
            },
          }),
        });
      }
      if (url.pathname === "/api/me") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j1",
            data: { id: "visitor-1", name: "Alice Explorer", role: "CUSTOMER", paymentIdentifier: "alice@transactx" },
          }),
        });
      }
      if (url.pathname === "/api/accounts") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j1",
            data: [{ id: "acc-1", accountNumber: "TX-1001", balancePaise: 500000, status: "ACTIVE", bankName: "Bank A", bankCode: "BANK-A" }],
          }),
        });
      }
      if (url.pathname.startsWith("/api/payments")) {
        return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "j1", data: [] }) });
      }
      return route.fallback();
    });

    // 1. Visit landing
    await page.goto("/");
    await expect(page.getByRole("heading", { level: 1 })).toContainText("Money moves.");

    // 2. Open demo gateway
    await page.getByRole("link", { name: "Explore the demo" }).click();
    await expect(page).toHaveURL(/\/demo/);
    await expect(page.getByRole("heading", { name: /One system/ })).toBeVisible();

    // 3. Click explore customer
    await page.getByRole("button", { name: "Explore customer" }).click();

    // 4. Customer dashboard
    await expect(page.getByRole("heading", { name: "Good day, Alice" })).toBeVisible();
    await expect(page.getByLabel("Account balance")).toContainText("₹5,000.00");

    // 5. Logout
    await page.getByRole("button", { name: "Log out of TransactX" }).click();
    await expect(page.getByRole("heading", { name: "Sign in to continue" })).toBeVisible();
    expect(await page.evaluate(() => sessionStorage.getItem("transactx.token"))).toBeNull();
  });

  test("Journey 2: Customer payment flow & receipt inspection", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });

    const submittedPayments: unknown[] = [];
    const idempotencyKeys: string[] = [];

    await page.route("http://localhost:8080/**", async (route) => {
      const url = new URL(route.request().url());
      if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });

      if (url.pathname === "/api/me") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j2",
            data: { id: "cust-1", name: "Alice Payer", role: "CUSTOMER", paymentIdentifier: "alice@transactx" },
          }),
        });
      }
      if (url.pathname === "/api/accounts") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j2",
            data: [{ id: "acc-1", accountNumber: "TX-1001", balancePaise: 500000, status: "ACTIVE", bankName: "Bank A", bankCode: "BANK-A" }],
          }),
        });
      }
      if (url.pathname.startsWith("/api/recipients/")) {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j2",
            data: { name: "Studio Store", paymentIdentifier: "store@transactx", status: "ACTIVE" },
          }),
        });
      }
      if (url.pathname === "/api/payments" && route.request().method() === "POST") {
        submittedPayments.push(route.request().postDataJSON());
        idempotencyKeys.push(route.request().headers()["idempotency-key"] ?? "");
        const created = {
          id: "22222222-2222-4222-8222-222222222222",
          amountPaise: 12500,
          currency: "INR",
          state: "COMPLETED",
          senderName: "Alice Payer",
          senderPaymentIdentifier: "alice@transactx",
          receiverName: "Studio Store",
          receiverPaymentIdentifier: "store@transactx",
          direction: "SENT",
          sourceBankCode: "BANK-A",
          destinationBankCode: "BANK-B",
          createdAt: new Date().toISOString(),
          completedAt: new Date().toISOString(),
          durationMs: 42,
        };
        return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "j2", data: created }) });
      }
      if (url.pathname.startsWith("/api/payments/22222222-2222-4222-8222-222222222222")) {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j2",
            data: {
              id: "22222222-2222-4222-8222-222222222222",
              amountPaise: 12500,
              currency: "INR",
              state: "COMPLETED",
              senderName: "Alice Payer",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Studio Store",
              receiverPaymentIdentifier: "store@transactx",
              direction: "SENT",
              sourceBankCode: "BANK-A",
              destinationBankCode: "BANK-B",
              createdAt: new Date().toISOString(),
              completedAt: new Date().toISOString(),
              durationMs: 42,
              origin: "WEB_PORTAL",
            },
          }),
        });
      }
      if (url.pathname.startsWith("/api/payments")) {
        return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "j2", data: [] }) });
      }
      return route.fallback();
    });

    await page.addInitScript(() => sessionStorage.setItem("transactx.token", "customer-j2-token"));
    await page.goto("/");

    // Navigate to pay
    await page.getByRole("button", { name: "Send money" }).first().click();
    await expect(page.getByRole("heading", { name: "Send money" })).toBeVisible();

    // Fill recipient
    await page.getByLabel("Recipient TransactX payment ID").fill("store@transactx");
    await page.getByRole("button", { name: "Find" }).click();
    await expect(page.getByText("Studio Store")).toBeVisible();

    // Fill amount & review
    await page.getByLabel("Amount in Indian rupees").fill("125.00");
    await page.getByRole("button", { name: "Review payment" }).click();

    await expect(page.getByRole("heading", { name: "Confirm payment details" })).toBeVisible();
    await page.getByRole("button", { name: "Confirm payment" }).click();

    // Verify payment completion result state and click through to audit receipt
    await expect(page.getByRole("heading", { name: "Payment complete" })).toBeVisible();
    await page.getByRole("button", { name: "View transaction" }).click();

    // Verify receipt drawer
    await expect(page.getByRole("heading", { name: "Payment details" })).toBeVisible();
    await expect(page.getByText("22222222-2222-4222-8222-222222222222")).toBeVisible();
    expect(submittedPayments.length).toBe(1);
    expect(idempotencyKeys.length).toBe(1);
    expect(idempotencyKeys[0]).toMatch(/^[0-9a-f-]{36}$/);
  });

  test("Journey 4: Merchant receive station, raw QR verification, incoming feed", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.route("http://localhost:8080/**", async (route) => {
      const url = new URL(route.request().url());
      if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });

      if (url.pathname === "/api/me") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j4",
            data: { id: "merch-1", name: "Metro Retail", role: "MERCHANT", paymentIdentifier: "metro@transactx" },
          }),
        });
      }
      if (url.pathname === "/api/accounts") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j4",
            data: [{ id: "acc-m1", accountNumber: "TX-M1001", balancePaise: 890000, status: "ACTIVE", bankName: "Bank B", bankCode: "BANK-B" }],
          }),
        });
      }
      if (url.pathname === "/api/merchant/receive-info") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j4",
            data: { paymentIdentifier: "metro@transactx", accountNumber: "TX-M1001", accountStatus: "ACTIVE" },
          }),
        });
      }
      if (url.pathname.startsWith("/api/payments")) {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "j4",
            data: [
              {
                id: "33333333-3333-4333-8333-333333333333",
                amountPaise: 25000,
                currency: "INR",
                state: "COMPLETED",
                senderName: "Customer Bob",
                senderPaymentIdentifier: "bob@transactx",
                receiverName: "Metro Retail",
                receiverPaymentIdentifier: "metro@transactx",
                direction: "RECEIVED",
                sourceBankCode: "BANK-A",
                destinationBankCode: "BANK-B",
                createdAt: new Date().toISOString(),
              },
            ],
          }),
        });
      }
      return route.fallback();
    });

    await page.addInitScript(() => sessionStorage.setItem("transactx.token", "merchant-j4-token"));
    await page.goto("/merchant");

    await expect(page.getByRole("heading", { name: "Welcome, Metro" })).toBeVisible();

    // Navigate to receive station
    await page.getByRole("button", { name: "Receive payment", exact: true }).first().click();
    await expect(page.getByRole("heading", { name: "Accept payments" })).toBeVisible();

    // Verify raw QR payment identifier
    const qrImage = page.getByRole("img", { name: "QR code containing payment ID metro@transactx" });
    await expect(qrImage).toBeVisible();
    await expect(page.getByText("This is not a UPI or bank-app QR.", { exact: false })).toBeVisible();

    // Download QR
    const downloadPromise = page.waitForEvent("download");
    await page.getByRole("button", { name: "Download PNG", exact: true }).click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe("transactx-receive.png");

    // Check incoming feed
    await page.goto("/merchant/incoming");
    await expect(page.getByRole("heading", { name: "Payments received" })).toBeVisible();
    await expect(page.getByText("Customer Bob")).toBeVisible();
    await expect(page.getByText("₹250.00")).toBeVisible();
  });

  test("Journey 5: Operations Viewer restrictions & admin route denial", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await installFixtureApi(page, "OPS_VIEWER");

    await page.goto("/console/worklist");
    await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();

    // Verify viewer navigation has zero admin views
    await expect(page.getByRole("button", { name: "Controlled faults" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Reconciliation" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Overview" })).toHaveCount(0);

    // Attempt direct URL to an admin route: /console/chaos
    await page.goto("/console/chaos");
    // Should be redirected immediately back to worklist
    await expect(page).toHaveURL(/\/console\/worklist/);
    await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();

    // Attempt direct URL to /console
    await page.goto("/console");
    await expect(page).toHaveURL(/\/console\/worklist/);
  });

  test("Journey 6: Operations Administrator comprehensive workbench inspection", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await adminFixtures(page);

    await page.goto("/console");
    await expect(page.getByRole("heading", { name: "Network overview" })).toBeVisible();
    await expect(page.getByText("Execution targets")).toBeVisible();

    // Routing
    await page.goto("/console/routing");
    await expect(page.getByRole("heading", { name: "Target Circuit Controls" })).toBeVisible();
    await expect(page.getByText("RAIL-A").first()).toBeVisible();

    // Chaos
    await page.goto("/console/chaos");
    await expect(page.getByRole("heading", { name: "Chaos Simulation Controller" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Engage Scenario" })).toBeVisible();

    // Activity
    await page.goto("/console/activity");
    await expect(page.getByRole("heading", { name: "Operational Activity Log" })).toBeVisible();
  });

  test("Journey 7: Cross-role session isolation (OPS_ADMIN -> MERCHANT)", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await adminFixtures(page);

    // 1. Enter as admin
    await page.goto("/console");
    await expect(page.getByRole("heading", { name: "Network overview" })).toBeVisible();

    // 2. Logout
    await page.getByRole("button", { name: "Log out" }).click();
    await expect(page.getByRole("heading", { name: "Sign in to continue" })).toBeVisible();
    expect(await page.evaluate(() => sessionStorage.getItem("transactx.token"))).toBeNull();

    // 3. Login as Merchant
    await page.route("http://localhost:8080/**", async (route) => {
      const url = new URL(route.request().url());
      if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
      if (url.pathname === "/api/me") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({ requestId: "j7", data: { id: "m7", name: "Shop", role: "MERCHANT", paymentIdentifier: "shop@transactx" } }),
        });
      }
      if (url.pathname === "/api/accounts") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({ requestId: "j7", data: [{ id: "acc-m7", accountNumber: "TX-77", balancePaise: 10000, status: "ACTIVE" }] }),
        });
      }
      if (url.pathname.startsWith("/api/payments")) {
        return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "j7", data: [] }) });
      }
      return route.fallback();
    });

    await page.evaluate(() => {
      sessionStorage.setItem("transactx.token", "merchant-jwt-token");
    });
    await page.goto("/merchant");

    // 4. Verify merchant workspace is clean without any admin elements
    await expect(page.getByRole("heading", { name: "Welcome, Shop" })).toBeVisible();
    await expect(page.getByText("Controlled faults")).toHaveCount(0);
    await expect(page.getByText("Merkle tree")).toHaveCount(0);
    await expect(page.getByText("Circuit controls")).toHaveCount(0);
  });

  test("Journey 8: Responsive deep linking at 390px mobile viewport", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await installFixtureApi(page, "OPS_VIEWER");

    // Directly load mobile worklist
    await page.goto("/console/worklist");
    await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);

    // Reload mobile worklist
    await page.reload();
    await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);

    // Open mobile nav drawer
    await page.getByRole("button", { name: "Open operations navigation" }).click();
    const dialog = page.getByRole("dialog", { name: "Operations navigation" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Controlled faults" })).toHaveCount(0);
    await dialog.getByRole("button", { name: "Close navigation" }).click();
    await expect(dialog).not.toBeVisible();
  });
});
