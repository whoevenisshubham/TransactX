import { test, expect, chromium } from "@playwright/test";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { installFixtureApi, paymentId, fulfillFixture } from "./support/opsFixtures";

// Actual Chromium tab zoom, verified through chrome.tabs.getZoom and CSS viewport
// reflow. Device pixel ratio, CSS zoom and pinch scaling are not substitutes.
for (const theme of ["light", "dark"] as const) test(`200% browser zoom preserves review, evidence and keyboard navigation in ${theme}`, async () => {
  const root = await mkdtemp(join(tmpdir(), "transactx-zoom-"));
  const extension = join(root, "extension");
  await mkdir(extension);
  await writeFile(join(extension, "manifest.json"), JSON.stringify({ manifest_version: 3, name: "Local TransactX zoom verification", version: "1.0", permissions: ["tabs"], background: { service_worker: "worker.js" } }));
  await writeFile(join(extension, "worker.js"), "chrome.runtime.onInstalled.addListener(() => {});");
  const context = await chromium.launchPersistentContext("", {
    channel: "chromium", headless: true, viewport: { width: 1440, height: 1000 },
    colorScheme: theme, reducedMotion: "reduce",
    args: [`--disable-extensions-except=${extension}`, `--load-extension=${extension}`],
  });
  try {
    const worker = context.serviceWorkers()[0] ?? await context.waitForEvent("serviceworker");
    let page = context.pages()[0] ?? await context.newPage();
    await page.addInitScript(() => sessionStorage.setItem("transactx.token", "fixture-only"));
    await page.route("http://localhost:8080/**", async route => {
      const path = new URL(route.request().url()).pathname;
      if (route.request().method() === "OPTIONS") return route.fulfill({status: 204, headers: {"access-control-allow-origin":"http://127.0.0.1:4173", "access-control-allow-headers":"Authorization, Content-Type"}});
      if (path === "/api/me") return fulfillFixture(route, {id:"zoom-fixture", name:"Alice Fixture", role:"CUSTOMER", paymentIdentifier:"alice@fixture"});
      if (path === "/api/accounts") return fulfillFixture(route, [{accountNumber:"TX-ZOOM-FIXTURE", balancePaise:985250, status:"ACTIVE", bankName:"Bank A Fixture", bankCode:"BANK-A"}]);
      if (path.startsWith("/api/recipients/")) return fulfillFixture(route, {name:"Demo Store Fixture",paymentIdentifier:"store@fixture",status:"ACTIVE"});
      if (path === "/api/payments") return fulfillFixture(route, []);
      throw new Error(`Unexpected zoom fixture endpoint ${path}`);
    });
    await page.goto("http://127.0.0.1:4173/");
    const zoom = await worker.evaluate(`(async () => {
      const [tab] = await chrome.tabs.query({url: "http://127.0.0.1:4173/*"});
      await chrome.tabs.setZoom(tab.id, 2);
      return await chrome.tabs.getZoom(tab.id);
    })()`);
    expect(zoom).toBe(2);
    await expect.poll(() => page.evaluate(() => innerWidth)).toBe(720);
    const inspect = async (screen: string) => {
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      if (process.env.TX_VISUAL_DIR) {
        // Full-page clipping uses CSS dimensions and truncates real tab zoom.
        // Capture the actual browser surface without changing its zoom/reflow.
        const session = await context.newCDPSession(page);
        const capture = await session.send("Page.captureScreenshot", {format:"png", fromSurface:true, captureBeyondViewport:false});
        await writeFile(`${process.env.TX_VISUAL_DIR}/v3-zoom200-${screen}-${theme}.png`, Buffer.from(capture.data,"base64"));
        await session.detach();
      }
    };
    await expect(page.getByRole("heading",{name:"Good day, Alice"})).toBeVisible();
    await inspect("customer-overview");
    await page.getByRole("button",{name:"Send money",exact:true}).first().click();
    await page.getByLabel("Recipient TransactX payment ID").fill("store@fixture");
    await page.getByRole("button",{name:"Find",exact:true}).click();
    await page.getByLabel("Amount in Indian rupees").fill("125.00");
    await page.getByRole("button",{name:"Review payment"}).click();
    const confirm = page.getByRole("button",{name:"Confirm payment",exact:true});
    await expect(confirm).toBeVisible();
    await page.getByRole("heading",{name:"Confirm payment details"}).scrollIntoViewIfNeeded();
    await inspect("payment-review-heading");
    await confirm.focus();
    await expect(confirm).toBeFocused();
    await inspect("payment-review");
    // Same actual 200% tab zoom; no API/worker or live money is involved.
    await page.route("http://localhost:8080/api/me", route => fulfillFixture(route,{id:"merchant-zoom-fixture",name:"Store Fixture",role:"MERCHANT",paymentIdentifier:"store@fixture"}));
    await page.goto("http://127.0.0.1:4173/merchant");
    await expect(page.getByRole("heading",{name:"Welcome, Store"})).toBeVisible();
    await inspect("merchant-dashboard");
    // No payment is submitted. Replace only the isolated API fixtures for ops.
    await page.unroute("http://localhost:8080/**");
    await installFixtureApi(page,"OPS_VIEWER");
    await page.goto("http://127.0.0.1:4173/ops");
    await page.getByRole("link",{name:paymentId}).click();
    await expect(page.getByRole("heading",{name:"Payment journey"})).toBeVisible();
    await page.getByRole("link",{name:"Ask about this payment ↓"}).click();
    await expect(page.getByRole("complementary",{name:"Investigation workspace"})).toBeFocused();
    await inspect("evidence");
    const menu=page.getByRole("button",{name:"Open operations navigation"});
    await menu.focus(); await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog",{name:"Operations navigation"})).toBeVisible();
    await inspect("navigation");
    await page.keyboard.press("Escape");
    await expect(menu).toBeFocused();
    // A fresh page has no page-scoped auth fixture init scripts. Clearing storage
    // on the old page would simply reinject its operator token on navigation.
    await page.close();
    page = await context.newPage();
    await page.goto("http://127.0.0.1:4173/");
    expect(await worker.evaluate(`(async () => {
      const [tab] = await chrome.tabs.query({url: "http://127.0.0.1:4173/*"});
      await chrome.tabs.setZoom(tab.id, 2);
      return await chrome.tabs.getZoom(tab.id);
    })()`)).toBe(2);
    await expect.poll(() => page.evaluate(() => innerWidth)).toBe(720);
    await expect(page.getByRole("heading",{level:1})).toContainText("Money moves.");
    await inspect("landing");
  } finally { await context.close(); }
});
