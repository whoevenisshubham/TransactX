import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

for (const theme of ["light", "dark"] as const) {
  for (const width of [1440, 390]) {
    test(`account access and demo chooser in ${theme} at ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.emulateMedia({ colorScheme: theme });
      await page.route("http://localhost:8080/**", async route => {
        const headers = { "access-control-allow-origin": "http://127.0.0.1:4173", "access-control-allow-headers": "Content-Type", "access-control-allow-methods": "GET, POST, OPTIONS" };
        if (route.request().method() === "OPTIONS") return route.fulfill({ status: 204, headers });
        if (route.request().url().endsWith("/api/auth/options")) return route.fulfill({ headers, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", data: { demoEnabled: true, registrationEnabled: false, googleEnabled: false } }) });
        return route.fulfill({ status: 403, headers, contentType: "application/json", body: JSON.stringify({ requestId: "fixture", error: { code: "DEMO_DISABLED", message: "Disabled" } }) });
      });
      await page.goto("/login");
      await expect(page.getByRole("heading", { name: "Sign in to continue" })).toBeVisible();
      await expect(page.getByRole("tab", { name: "Create account" })).toHaveCount(0);
      await page.getByLabel("Password", { exact: true }).fill("fixture-not-a-secret");
      await page.getByRole("button", { name: "Show password" }).click();
      await expect(page.getByLabel("Password", { exact: true })).toHaveAttribute("type", "text");
      await page.getByRole("button", { name: "Hide password" }).click();
      await expect(page.getByLabel("Password", { exact: true })).toHaveAttribute("type", "password");
      for (const path of ["/login", "/demo?role=operator", "/privacy"]) {
        await page.goto(path);
        await page.locator("main").waitFor();
        const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
        expect(scan.violations.filter(item => item.impact === "serious" || item.impact === "critical").map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      }
      await page.goto("/demo?role=operator");
      const payloads: unknown[] = [];
      page.on("request", request => { if (request.url().endsWith("/api/auth/demo") && request.method() === "POST") payloads.push(request.postDataJSON()); });
      await page.getByRole("button", { name: "Explore operator" }).click();
      await expect(page.getByRole("alert")).toContainText("Shared demo access is disabled");
      expect(payloads).toEqual([{ persona: "operator" }]);
      await expect(page.getByRole("button", { name: "Explore operator" })).toBeEnabled();
      expect(await page.evaluate(() => sessionStorage.getItem("transactx.token"))).toBeNull();
    });
  }
}
