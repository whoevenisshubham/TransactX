import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { resolve } from "node:path";
import type { Payment } from "../src/types";

const payment: Payment = { id: "11111111-1111-4111-8111-111111111111", amountPaise: 12500, currency: "INR", note: "Illustrative fixture lunch", origin: "BANK", state: "COMPLETED", createdAt: "2026-10-18T08:00:00Z", completedAt: "2026-10-18T08:00:01Z", senderName: "Alice Fixture", senderPaymentIdentifier: "alice@fixture", receiverName: "Demo Store Fixture", receiverPaymentIdentifier: "store@fixture", direction: "RECEIVED", sourceBankCode: "BANK-A", destinationBankCode: "BANK-B" };

for (const theme of ["light", "dark"] as const) for (const width of [1440, 390]) {
  test(`merchant dashboard, receive, incoming, settlement and search in ${theme} at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
    await page.emulateMedia({ colorScheme: theme });
    await page.addInitScript(() => sessionStorage.setItem("transactx.token", "merchant-fixture-only"));
    await page.route("http://localhost:8080/**", async route => {
      const path = new URL(route.request().url()).pathname;
      const headers = { "access-control-allow-origin": "http://127.0.0.1:4173", "access-control-allow-headers": "Authorization, Content-Type", "access-control-allow-methods": "GET, OPTIONS" };
      if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
      let data: unknown;
      if (path === "/api/me") data = { id: "store-fixture", name: "Store Fixture", role: "MERCHANT", paymentIdentifier: "store@fixture" };
      else if (path === "/api/accounts") data = [{ accountNumber: "TX-FIXTURE-STORE", balancePaise: 12500, status: "ACTIVE" }];
      else if (path === "/api/payments") data = [payment, { ...payment, id: "22222222-2222-4222-8222-222222222222", state: "PENDING_RECONCILIATION", completedAt: undefined, amountPaise: 2500 }];
      else if (path === "/api/merchant/receive-info") data = { paymentIdentifier: "store@fixture", accountNumber: "TX-FIXTURE-STORE", accountStatus: "ACTIVE" };
      else throw new Error(`Unexpected merchant fixture endpoint ${path}`);
      await route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", data }) });
    });
    async function inspect(screen: string) {
      const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
      expect(scan.violations.filter(item => item.impact === "serious" || item.impact === "critical").map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      if (process.env.TX_VISUAL_DIR) await page.screenshot({ path: `${process.env.TX_VISUAL_DIR}/v3-merchant-${screen}-${theme}-${width}.png`, fullPage: true });
    }
    await page.goto("/merchant");
    await expect(page.getByRole("heading", { name: "Welcome, Store" })).toBeVisible();
    await expect(page.getByText("1 completed payment in this page")).toBeVisible();
    await inspect("dashboard");
    await page.getByRole("button", { name: "Receive payment", exact: true }).first().click();
    await expect(page.locator(".merchant-qr-frame svg")).toBeVisible();
    await expect(page.getByText("This is not a UPI or bank-app QR.", { exact: false })).toBeVisible();
    await page.evaluate(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async () => { throw new DOMException("Denied", "NotAllowedError"); } } }));
    await page.getByRole("button", { name: "Copy payment ID", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("Copy unavailable. Select the value to copy.");
    await page.evaluate(() => Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText: async (value: string) => { if (value !== "store@fixture") throw new Error("Wrong identifier"); } } }));
    await page.getByRole("button", { name: "Copy payment ID", exact: true }).click();
    await expect(page.getByRole("status")).toHaveText("Copied");
    await inspect("receive");
    await page.addScriptTag({ path: resolve("node_modules/jsqr/dist/jsQR.js") });
    for (const format of ["svg", "png"]) {
      const event = page.waitForEvent("download");
      await page.getByRole("button", { name: `Download ${format.toUpperCase()}` }).click();
      const download = await event;
      expect(download.suggestedFilename()).toBe(`transactx-receive.${format}`);
      const stream = await download.createReadStream();
      const chunks: Buffer[] = [];
      for await (const chunk of stream) chunks.push(Buffer.from(chunk));
      const uri = `data:image/${format === "svg" ? "svg+xml" : "png"};base64,${Buffer.concat(chunks).toString("base64")}`;
      const decoded = await page.evaluate(async source => {
        const image = new Image(); image.src = source; await image.decode();
        const canvas = document.createElement("canvas"); canvas.width = canvas.height = 768;
        const context = canvas.getContext("2d")!; context.fillStyle = "#fff"; context.fillRect(0, 0, 768, 768); context.drawImage(image, 0, 0, 768, 768);
        const decoder = window as Window & { jsQR: (pixels: Uint8ClampedArray, width: number, height: number) => { data: string } | null };
        return decoder.jsQR(context.getImageData(0, 0, 768, 768).data, 768, 768)?.data;
      }, uri);
      expect(decoded).toBe("store@fixture");
    }
    await page.goto("/merchant/incoming");
    await expect(page.getByRole("heading", { name: "Payments received" })).toBeVisible();
    await inspect("incoming");
    await page.goto("/merchant/settlement");
    await expect(page.getByRole("heading", { name: "Settlement status" })).toBeVisible();
    await expect(page.getByText("Not yet confirmed", { exact: true })).toBeVisible();
    await inspect("settlement");
    await page.goto("/merchant/search");
    await expect(page.getByRole("heading", { name: "Transaction search" })).toBeVisible();
    await page.getByLabel("Search by sender or note").fill(payment.id);
    await expect(page.getByText("1 result found", { exact: true })).toBeVisible();
    await inspect("search");
  });
}
