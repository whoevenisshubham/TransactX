import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { installFixtureApi, fulfillFixture, fixture, worklistFixture, paymentId } from "./support/opsFixtures";

const widths = process.env.TX_QA_WIDTHS ? process.env.TX_QA_WIDTHS.split(",").map(Number) : [1440, 390];
if (widths.some(width => !Number.isInteger(width) || width < 320 || width > 1920)) throw new Error("Invalid QA width");
for (const theme of ["light", "dark"] as const) for (const width of widths) {
  test(`operator worklist, journey, report and window charts in ${theme} at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 1440 ? 900 : 844 });
    await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
    await installFixtureApi(page, "OPS_VIEWER");
    await page.route("http://localhost:8080/api/ops/worklist**", async route => {
      if (route.request().method() !== "GET") return route.fallback();
      const data = worklistFixture().data;
      if (new URL(route.request().url()).searchParams.get("state") === "COMPLETED") data.items[0].state = "COMPLETED";
      return fulfillFixture(route,data);
    });
    await page.route("http://localhost:8080/api/ops/analytics/windows**", async route => {
      if (route.request().method() !== "GET") return route.fallback();
      const items = ["BANK-A fixture", "BANK-B fixture"].flatMap(entityId => Array.from({ length: 6 }, (_, index) => ({
        id: `${entityId}-${index}`, entityType: "bank", entityId, from: new Date(Date.UTC(2026,9,11,8,index * 10)).toISOString(), to: new Date(Date.UTC(2026,9,11,8,index * 10 + 5)).toISOString(), sampleCount: 24, featureSchemaVersion: "fixture.v1", features: { timeoutRate: index === 2 ? null : index / 100, failureRate: index === 3 ? null : index / 200, p95LatencyMs: index === 2 ? null : 120 + index * 8, oldestRecoveryAgeMs: null, recoveryAttemptsPerPayment: null, healthAvailabilityRate: null }, baselineAlert: null, model: { version: null, status: "unavailable", score: null, threshold: null }, advisory: true,
      })));
      return fulfillFixture(route,{ items, asOf: "2026-10-11T09:00:00Z", nextCursor:null });
    });
    async function inspect(screen: string) {
      const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
      expect(scan.violations.filter(item => item.impact === "serious" || item.impact === "critical").map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      if (process.env.TX_VISUAL_DIR) await page.screenshot({ path: `${process.env.TX_VISUAL_DIR}/v3-operator-${screen}-${theme}-${width}.png`, fullPage:screen !== "navigation" });
    }
    await page.goto("/console/worklist");
    await expect(page.getByRole("heading",{name:"Recovery worklist"})).toBeVisible();
    await page.getByRole("button",{name:"Completed",exact:true}).click();
    await expect(page).toHaveURL(/state=COMPLETED/);
    await expect(page.getByText("1 completed on this page")).toBeVisible();
    await inspect("worklist");
    await page.getByRole("link",{name:paymentId}).click();
    await expect(page.getByRole("heading",{name:"Payment journey"})).toBeVisible();
    await page.getByRole("button",{name:"What evidence is missing?"}).click();
    await expect(page.getByLabel("Question",{exact:true})).toHaveValue("What evidence is missing?");
    if (width < 960) {
      await page.getByRole("link", { name: "Ask about this payment ↓" }).click();
      await expect(page.getByRole("complementary", { name: "Investigation workspace" })).toBeFocused();
      await expect(page.getByLabel("Question", { exact: true })).toHaveValue("What evidence is missing?");
    }
    await page.getByRole("button",{name:"Start investigation",exact:true}).click();
    await expect(page.getByText("The central payment is PENDING_RECONCILIATION.")).toBeVisible();
    await inspect("journey-report");
    await page.getByRole("button",{name:"Open saved investigation"}).click();
    await expect(page.getByRole("heading",{name:"Saved investigation"})).toBeVisible();
    await inspect("saved-report");
    if (width < 960) {
      await page.getByRole("button",{name:"Open operations navigation"}).click();
      const dialog=page.getByRole("dialog",{name:"Operations navigation"});
      await expect(dialog).toBeVisible();
      await expect(dialog.getByRole("button",{name:"Controlled faults"})).toHaveCount(0);
      await inspect("navigation");
      await page.keyboard.press("Escape");
      await expect(dialog).not.toBeVisible();
      await expect(page.getByRole("button",{name:"Open operations navigation"})).toBeFocused();
      await page.getByRole("button",{name:"Open operations navigation"}).click();
      await dialog.getByRole("button",{name:"Analytics",exact:true}).click();
      await expect(dialog).not.toBeVisible();
    } else await page.getByRole("button",{name:"Analytics",exact:true}).click();
    await expect(page.getByRole("heading",{name:"Operational analytics"})).toBeVisible();
    await expect(page.getByRole("img",{name:/Timeout and failure rate/})).toBeVisible();
    await expect(page.getByText("Statistical baseline is the default.")).toBeVisible();
    await page.getByLabel("Entity and window").selectOption({label:"bank BANK-B fixture · 5 min · fixture.v1"});
    await expect(page.getByRole("img",{name:/Timeout and failure rate, bank BANK-B fixture/})).toBeVisible();
    await inspect("analytics");
  });
}
