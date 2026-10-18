import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import type { Payment } from "../src/types";

const payment: Payment = { id: "11111111-1111-4111-8111-111111111111", amountPaise: 12500, currency: "INR", note: "Fixture lunch", origin: "BANK", state: "COMPLETED", createdAt: "2026-10-18T08:00:00Z", completedAt: "2026-10-18T08:00:01Z", senderName: "Alice Fixture", senderPaymentIdentifier: "alice@fixture", receiverName: "Demo Store Fixture", receiverPaymentIdentifier: "store@fixture", direction: "SENT", sourceBankCode: "BANK-A", sourceBankName: "Bank A Fixture", destinationBankCode: "BANK-B", destinationBankName: "Bank B Fixture" };

for (const theme of ["light", "dark"] as const) for (const width of [1440, 390]) {
  test(`customer overview, review, activity and receipt in ${theme} at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
    await page.emulateMedia({ colorScheme: theme });
    await page.addInitScript(() => sessionStorage.setItem("transactx.token", "fixture-only"));
    let submitted = 0;
    await page.route("http://localhost:8080/**", async route => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      const headers = { "access-control-allow-origin": "http://127.0.0.1:4173", "access-control-allow-headers": "Authorization, Content-Type, Idempotency-Key, X-Request-ID", "access-control-allow-methods": "GET, POST, OPTIONS" };
      if (request.method() === "OPTIONS") return route.fulfill({ status: 204, headers });
      let data: unknown;
      if (path === "/api/me") data = { id: "alice-fixture", name: "Alice Fixture", role: "CUSTOMER", paymentIdentifier: "alice@fixture" };
      else if (path === "/api/accounts") data = [{ accountNumber: "TX-FIXTURE-0001", balancePaise: 985250, status: "ACTIVE" }];
      else if (path.startsWith("/api/recipients/")) data = { name: "Demo Store Fixture", paymentIdentifier: "store@fixture", status: "ACTIVE" };
      else if (path === "/api/payments" && request.method() === "POST") {
        submitted++;
        expect(request.headers()["idempotency-key"]).toBeTruthy();
        expect(request.postDataJSON()).toMatchObject({ recipient: "store@fixture", amountPaise: 12500, currency: "INR" });
        data = payment;
      } else if (path === "/api/payments") data = [payment, { ...payment, id: "22222222-2222-4222-8222-222222222222", state: "PENDING_RECONCILIATION", completedAt: undefined, createdAt: "2026-10-17T08:00:00Z" }];
      else if (path === `/api/payments/${payment.id}`) data = payment;
      else throw new Error(`Unexpected fixture endpoint ${path}`);
      await route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", data }) });
    });
    async function inspect(screen: string) {
      const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
      expect(scan.violations.filter(item => item.impact === "serious" || item.impact === "critical").map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      if (process.env.TX_VISUAL_DIR) await page.screenshot({ path: `${process.env.TX_VISUAL_DIR}/v3-customer-${screen}-${theme}-${width}.png`, fullPage: true });
    }
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Good day, Alice" })).toBeVisible();
    await page.getByRole("button", { name: "Hide balance" }).click();
    await expect(page.getByLabel("Balance hidden")).toBeVisible();
    await page.getByRole("button", { name: "Show balance" }).click();
    await inspect("overview");
    await page.getByRole("button", { name: "Send money", exact: true }).first().click();
    await page.getByLabel("Recipient TransactX payment ID").fill("store@fixture");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await expect(page.getByText("Demo Store Fixture", { exact: true })).toBeVisible();
    await page.getByLabel("Amount in Indian rupees").fill("125.00");
    await inspect("send");
    await page.getByRole("button", { name: "Review payment" }).click();
    await expect(page.getByRole("heading", { name: "Confirm payment details" })).toBeVisible();
    await inspect("review");
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();
    await expect(page.getByRole("heading", { name: /Payment complete/ })).toBeVisible();
    expect(submitted).toBe(1);
    await inspect("outcome");
    await page.goto(`/transactions/${payment.id}`);
    await expect(page.getByRole("heading", { name: "Payment details" })).toBeVisible();
    await expect(page.getByText("Bank A Fixture (BANK-A)")).toBeVisible();
    await expect(page.getByRole("button", { name: "Copy payment reference" })).toBeVisible();
    await inspect("receipt");
    await page.goto("/transactions");
    await expect(page.getByRole("heading", { name: "Transactions", exact: true })).toBeVisible();
    await page.getByLabel("Status", { exact: true }).selectOption("COMPLETED");
    await page.getByLabel("From date (UTC)").fill("2026-10-18");
    await expect(page.getByText("Showing 1 of 2 transactions")).toBeVisible();
    await inspect("activity");
  });
}
