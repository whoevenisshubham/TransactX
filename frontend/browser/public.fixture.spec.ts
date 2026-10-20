import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

for (const theme of ["light", "dark"] as const) {
  for (const width of [1440, 390, 320]) {
    test(`public landing is static, accessible and contained at ${width} in ${theme}`, async ({ page }) => {
      await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
      await page.setViewportSize({ width, height: 900 });
      const apiRequests: string[] = [];
      page.on("request", request => { if (request.url().includes("/api/")) apiRequests.push(request.url()); });
      await page.goto("/");
      await expect(page.getByRole("heading", { level: 1 })).toContainText("Money moves.");
      await expect(page.getByText("Illustrative example")).toBeVisible();
      await expect(page.getByRole("tab", { name: "Customer", exact: true })).toHaveAttribute("aria-selected", "true");
      await expect(page.getByRole("link", { name: "Open customer demo" })).toHaveAttribute("href", "/demo?role=customer");
      await page.getByRole("tab", { name: "Customer", exact: true }).focus();
      await page.keyboard.press("ArrowRight");
      await expect(page.getByRole("tab", { name: "Merchant", exact: true })).toBeFocused();
      await expect(page.getByRole("link", { name: "Open merchant demo" })).toHaveAttribute("href", "/demo?role=merchant");
      await page.keyboard.press("ArrowRight");
      await expect(page.getByRole("tab", { name: "Operations", exact: true })).toHaveAttribute("aria-selected", "true");
      await expect(page.getByRole("link", { name: "Open operations demo" })).toHaveAttribute("href", "/demo?role=operator");
      await page.keyboard.press("Home");
      await expect(page.getByRole("tab", { name: "Customer", exact: true })).toBeFocused();
      await page.getByRole("link", { name: "TransactX home" }).focus();
      await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
      await page.keyboard.press("Shift+Tab");
      await expect(page.getByRole("link", { name: "Skip to content" })).toBeFocused();
      await page.keyboard.press("Enter");
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]).analyze();
      expect(scan.violations.filter(item => item.impact === "serious" || item.impact === "critical").map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, summary: node.failureSummary })) }))).toEqual([]);
      expect(apiRequests).toEqual([]);
    });
  }
}

test("explicit appearance survives reload and system changes until System is selected", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.goto("/");
  await page.getByLabel("Appearance").selectOption("dark");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.getByLabel("Appearance")).toHaveValue("dark");
  await page.emulateMedia({ colorScheme: "dark" });
  await page.emulateMedia({ colorScheme: "light" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.getByLabel("Appearance").selectOption("system");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.emulateMedia({ colorScheme: "dark" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
});

test("blocked preference storage leaves the public page usable", async ({ page }) => {
  await page.addInitScript(() => Object.defineProperty(window, "localStorage", { get() { throw new DOMException("Disabled", "SecurityError"); } }));
  await page.goto("/");
  await page.getByLabel("Appearance").selectOption("dark");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByRole("link", { name: "Explore the demo" })).toBeVisible();
});
