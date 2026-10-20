import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { installFixtureApi, paymentId, fixture } from "./support/opsFixtures";

// These captures are intercepted design fixtures, never live financial evidence.
const widths = (process.env.TX_QA_WIDTHS ?? "1440,390").split(",").map(Number);
if (widths.some(width => !Number.isInteger(width) || width < 320 || width > 1920)) throw new Error("Invalid capture width");
for (const theme of ["light", "dark"] as const) for (const width of widths) {
  for (const screen of ["landing", "customer", "merchant", "journey"] as const) {
    test(`visual direction ${screen} ${theme} ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: width > 960 ? 1000 : 844 });
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
      const apiRequests: string[] = [];
      page.on("request", request => { if (request.url().includes("/api/")) apiRequests.push(request.url()); });
      if (screen === "journey") await installFixtureApi(page, "OPS_VIEWER");
      else if (screen !== "landing") {
        await page.addInitScript(() => sessionStorage.setItem("transactx.token", "design-fixture-only"));
        await page.route("http://localhost:8080/**", async route => {
          const path = new URL(route.request().url()).pathname;
          const headers = { "access-control-allow-origin": "http://127.0.0.1:4173", "access-control-allow-headers": "Authorization,Content-Type", "access-control-allow-methods": "GET,OPTIONS" };
          if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
          const merchant = screen === "merchant";
          const identifier = merchant ? "store@fixture" : "alice@fixture";
          let data: unknown;
          if (path === "/api/me") data = { id: `${screen}-fixture`, name: merchant ? "Studio Store Fixture" : "Alice Fixture", role: merchant ? "MERCHANT" : "CUSTOMER", paymentIdentifier: identifier };
          else if (path === "/api/accounts") data = [{ id: "account-fixture", accountNumber: "TX-FIXTURE-0001", balancePaise: merchant ? 428750 : 985250, status: "ACTIVE", bankName: merchant ? "Bank B Fixture" : "Bank A Fixture", bankCode: merchant ? "BANK-B" : "BANK-A" }];
          else if (path === "/api/merchant/receive-info") data = { paymentIdentifier: identifier, accountNumber: "TX-FIXTURE-0001", accountStatus: "ACTIVE" };
          else if (path === "/api/payments") data = ["Studio Store", "Bob Demo", "Asha Demo", "Studio Store", "Bob Demo"].map((name,index) => ({ id: `${index + 1}1111111-1111-4111-8111-111111111111`, amountPaise: [12500,24000,8750,18000,3200][index], currency: "INR", state: index === 2 ? "PENDING_RECONCILIATION" : "COMPLETED", note: "Synthetic design fixture", createdAt: `2026-10-19T${String(12-index).padStart(2,"0")}:10:00Z`, senderName: merchant || index === 2 ? name : "Alice Fixture", senderPaymentIdentifier: merchant || index === 2 ? `${name.split(" ")[0].toLowerCase()}@fixture` : identifier, receiverName: merchant ? "Studio Store Fixture" : index === 2 ? "Alice Fixture" : name, receiverPaymentIdentifier: merchant || index === 2 ? identifier : `${name.split(" ")[0].toLowerCase()}@fixture`, direction: merchant ? "RECEIVED" : index === 2 ? "RECEIVED" : "SENT", sourceBankCode: "BANK-A", destinationBankCode: "BANK-B" }));
          else throw new Error(`Unexpected design fixture endpoint ${path}`);
          return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "design-fixture", data }) });
        });
      }
      await page.goto(screen === "merchant" ? "/merchant" : screen === "journey" ? `/console/payments/${paymentId}` : "/");
      await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
      if (screen === "journey") {
        await page.getByRole("button", { name: "What evidence is missing?" }).click();
        await page.getByRole("button", { name: "Start investigation", exact: true }).click();
        await expect(page.getByText("The central payment is PENDING_RECONCILIATION.")).toBeVisible();
      }
      await page.evaluate(() => document.fonts.ready);
      await page.evaluate(() => scrollTo(0,0));
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      if (process.env.TX_VISUAL_DIR) await page.screenshot({ path: `${process.env.TX_VISUAL_DIR}/${screen}-${theme}-${width}.png`, fullPage: true });
      if (process.env.TX_VISUAL_CHECKS === "true") {
        const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
        expect(scan.violations.filter(item => item.impact === "serious" || item.impact === "critical").map(item => ({ id: item.id, nodes: item.nodes.map(node=>({target:node.target,summary:node.failureSummary})) }))).toEqual([]);
        if (screen === "landing") {
          expect(apiRequests).toEqual([]);
          await page.getByRole("tab", { name: "Merchant", exact: true }).click();
          await expect(page.getByRole("tabpanel").getByRole("img", { name: /Merchant receiving desk/ }).filter({ visible: true })).toBeVisible();
          await page.getByRole("tab", { name: "Operations", exact: true }).click();
          await expect(page.getByRole("tabpanel").getByRole("img", { name: /Operations payment journey/ }).filter({ visible: true })).toBeVisible();
        }
        if (screen === "customer") {
          await page.getByRole("button", { name: "Hide balance", exact: true }).click();
          await expect(page.getByLabel("Balance hidden")).toBeVisible();
          await page.getByRole("button", { name: "Show balance", exact: true }).click();
          await page.getByRole("button", { name: "Received", exact: true }).click();
          await expect(page.locator(".payment-list .payment-row")).toHaveCount(1);
          await expect(page.locator(".payment-list")).toContainText("Asha Demo");
          await page.getByLabel("Find in loaded activity").fill("no matching name");
          await expect(page.getByRole("heading", { name: "No matching activity" })).toBeVisible();
        }
        if (screen === "merchant") {
          await expect(page.getByRole("img", { name: "QR code containing payment ID store@fixture" })).toBeVisible();
          const download = page.waitForEvent("download");
          await page.getByRole("button", { name: "Download QR", exact: true }).click();
          expect((await download).suggestedFilename()).toBe("transactx-receive.png");
        }
        if (screen === "journey") {
          await page.getByRole("button", { name: /^View evidence payment:/ }).click();
          const id = fixture("evidence-partial.json").data.payment.evidenceId;
          await expect(page.locator(`[id="evidence-${id}"]`)).toBeFocused();
        }
      }
    });
  }
}

test("zero-balance private account can deliberately switch to a funded shared demo", async ({ page }) => {
  await page.addInitScript(() => {
    if (!sessionStorage.getItem("zero-fixture-initialized")) {
      sessionStorage.setItem("zero-fixture-initialized", "true");
      sessionStorage.setItem("transactx.token", "private-zero-fixture");
    }
  });
  await page.route("http://localhost:8080/**", async route => {
    const path = new URL(route.request().url()).pathname;
    const headers = { "access-control-allow-origin": "http://127.0.0.1:4173", "access-control-allow-headers": "Authorization,Content-Type", "access-control-allow-methods": "GET,OPTIONS" };
    if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
    let data: unknown;
    if (path === "/api/me") data = { id: "private-zero-fixture", name: "Private Fixture", role: "CUSTOMER", paymentIdentifier: "private@fixture" };
    else if (path === "/api/accounts") data = [{ id: "private-account", accountNumber: "TX-PRIVATE-FIXTURE", balancePaise: 0, status: "ACTIVE", bankCode: "BANK-A" }];
    else if (path === "/api/payments") data = [];
    else throw new Error(`No login, provider or payment submission expected: ${path}`);
    return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", data }) });
  });
  await page.goto("/");
  await expect(page.getByText("Your available balance is ₹0.")).toBeVisible();
  await page.getByRole("button", { name: "Log out and explore shared demos" }).click();
  await expect(page).toHaveURL(/\/demo$/);
  await expect(page.getByRole("button", { name: "Explore customer", exact: true })).toBeVisible();
  expect(await page.evaluate(() => sessionStorage.getItem("transactx.token"))).toBeNull();
  expect(await page.evaluate(() => sessionStorage.getItem("transactx.profile"))).toBeNull();
});

