import { expect, test, chromium, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, join, resolve, sep } from "node:path";

function inputs() {
  const paymentId = process.env.TX_LIVE_PAYMENT_ID;
  const state = process.env.TX_LIVE_PAYMENT_STATE;
  const identity = process.env.TX_LIVE_VIEWER_ID;
  const password = process.env.TX_LIVE_PASSWORD;
  const frontend = process.env.TX_LIVE_FRONTEND_URL;
  const api = process.env.TX_LIVE_API_URL;
  if (process.env.TX_LIVE_DISPOSABLE_DB !== "1" || !paymentId || !state || !identity || !password ||
      ![frontend, api].every((origin) => origin && ["localhost", "127.0.0.1"].includes(new URL(origin).hostname))) {
    throw new Error("Live accessibility checks require disposable loopback API/frontend and real viewer/payment inputs.");
  }
  return { paymentId, state, identity, password, frontend: frontend! };
}

async function tabTo(page: Page, target: Locator) {
  await expect(target).toBeVisible();
  for (let step = 0; step < 80; step += 1) {
    if (await target.evaluate((element) => element === document.activeElement)) return;
    await page.keyboard.press("Tab");
  }
  throw new Error("Control was not reachable with the keyboard");
}

async function scan(page: Page) {
  const result = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
  expect(result.violations.filter((item) => item.impact === "critical" || item.impact === "serious")
    .map((item) => ({ id: item.id, nodes: item.nodes.map((node) => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
  const layout = await page.evaluate(() => ({ width: innerWidth, scrollWidth: document.documentElement.scrollWidth,
    overflow: [...document.querySelectorAll("body *")].map((element) => ({
      tag: element.tagName, className: element.className, right: element.getBoundingClientRect().right,
    })).filter((element) => element.right > innerWidth + 1).slice(0, 20) }));
  if (layout.scrollWidth > layout.width + 1) {
    await test.info().attach("overflow-layout", { body: JSON.stringify(layout), contentType: "application/json" });
    await page.screenshot({ path: test.info().outputPath("overflow.png"), fullPage: true });
  }
  expect(layout.scrollWidth).toBeLessThanOrEqual(layout.width + 1);
}

async function authenticatedViews(page: Page) {
  const { paymentId, state, identity, password, frontend } = inputs();
  await page.goto(`${frontend}/console/worklist?paymentId=${paymentId}&state=${encodeURIComponent(state)}`);
  await page.getByLabel("Phone or payment ID").fill(identity);
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Password")).toBeFocused();
  await page.keyboard.type(password);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Recovery worklist" })).toBeVisible();
  await scan(page);

  await tabTo(page, page.getByRole("link", { name: paymentId, exact: true }));
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  await scan(page);

  await tabTo(page, page.getByLabel("Question", { exact: true }));
  await page.keyboard.type("What collected evidence explains this payment?");
  await tabTo(page, page.getByRole("button", { name: "Start investigation", exact: true }));
  await page.keyboard.press("Enter");
  await expect(page.getByText("Status: completed · Mode: Evidence summary", { exact: true })).toBeVisible({ timeout: 60000 });
  await scan(page);
  await tabTo(page, page.getByRole("button", { name: "Open saved investigation", exact: true }));
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Facts", exact: true })).toBeVisible();
  await scan(page);
  const citation = page.getByRole("link", { name: /^(Payment record|Bank operation|State transition|Evidence) \[\d+\]/ }).first();
  const reference = new URL((await citation.getAttribute("href"))!, frontend);
  const evidenceId = decodeURIComponent(reference.hash.slice(1));
  expect(evidenceId.startsWith("evidence-")).toBe(true);
  await tabTo(page, citation);
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(reference.href);
  await expect(page.getByRole("heading", { name: "Payment journey" })).toBeVisible();
  const target = page.locator(`[id=${JSON.stringify(evidenceId)}]`);
  await expect(target).toBeVisible();
  await expect.poll(() => target.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    return bounds.top < innerHeight && bounds.bottom > 0;
  })).toBe(true);

  await tabTo(page, page.getByRole("button", { name: "Analytics", exact: true }));
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Operational analytics" })).toBeVisible();
  await expect(page.getByRole("table")).toBeVisible();
  await scan(page);
}

for (const width of [360, 768, 1440]) {
  test(`native operator screens support keyboard and accessibility at ${width}px`, async ({ page }) => {
    test.setTimeout(90000);
    await page.setViewportSize({ width, height: 1000 });
    await authenticatedViews(page);
  });
}

test("native operator screens remain accessible at actual 200% browser zoom", async ({}, testInfo) => {
  test.setTimeout(90000);
  const { frontend } = inputs();
  const directory = await mkdtemp(join(tmpdir(), "tx-m1-browser-zoom-"));
  const extension = join(directory, "extension");
  await mkdir(extension);
  await writeFile(join(extension, "manifest.json"), JSON.stringify({ manifest_version: 3,
    name: "M1 browser zoom verification", version: "1.0.0", permissions: ["tabs"],
    background: { service_worker: "background.js" } }));
  await writeFile(join(extension, "background.js"), "chrome.runtime.onInstalled.addListener(()=>{});");
  const context = await chromium.launchPersistentContext(join(directory, "profile"), {
    channel: process.env.CI ? undefined : "msedge", headless: true, viewport: null,
    ignoreDefaultArgs: ["--disable-extensions"],
    args: [...(testInfo.project.use.launchOptions?.args ?? []),
      `--disable-extensions-except=${extension}`, `--load-extension=${extension}`, "--window-size=1440,1000"],
  });
  try {
    const page = context.pages()[0] ?? await context.newPage();
    await page.goto(frontend);
    const before = await page.evaluate(() => ({ width: innerWidth, ratio: devicePixelRatio }));
    const worker = context.serviceWorkers().find((item) => item.url().startsWith("chrome-extension://")) ??
      await context.waitForEvent("serviceworker", { predicate: (item) => item.url().startsWith("chrome-extension://"), timeout: 10000 });
    const zoom = await worker.evaluate(async (origin) => {
      const chrome = (globalThis as unknown as { chrome: { tabs: {
        query: (query: object) => Promise<{ id: number; url?: string }[]>;
        setZoom: (id: number, zoom: number) => Promise<void>;
        getZoom: (id: number) => Promise<number>;
      } } }).chrome;
      const tabs = await chrome.tabs.query({});
      const tab = tabs.find((item) => item.url?.startsWith(origin));
      if (!tab) throw new Error("Application tab not found");
      await chrome.tabs.setZoom(tab.id, 2);
      return await chrome.tabs.getZoom(tab.id);
    }, frontend);
    expect(zoom).toBe(2);
    await expect.poll(() => page.evaluate(() => devicePixelRatio)).toBeCloseTo(before.ratio * 2, 1);
    const after = await page.evaluate(() => ({ width: innerWidth, ratio: devicePixelRatio }));
    expect(after.width).toBeLessThan(before.width * 0.6);
    const zoomEvidence = testInfo.outputPath("actual-zoom.json");
    await writeFile(zoomEvidence, JSON.stringify({ zoom, before, after, browser: context.browser()?.version() }));
    await testInfo.attach("actual-browser-zoom", { path: zoomEvidence, contentType: "application/json" });
    await authenticatedViews(page);
  } finally {
    await context.close();
    const target = resolve(directory);
    if (!target.startsWith(resolve(tmpdir()) + sep) || !basename(target).startsWith("tx-m1-browser-zoom-")) {
      throw new Error("Refusing to remove an unexpected browser profile directory");
    }
    await rm(target, { recursive: true, force: true });
  }
});
