import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { resolve } from "node:path";
import { mkdirSync } from "node:fs";

const viewports = [
  { name: "desktop-1440", width: 1440, height: 900 },
  { name: "tablet-1024", width: 1024, height: 768 },
  { name: "tablet-768", width: 768, height: 1024 },
  { name: "mobile-390", width: 390, height: 844 },
];

const screenshotDir = resolve(process.cwd(), "test-results", "showcase-previews");
try {
  mkdirSync(screenshotDir, { recursive: true });
} catch {}

for (const theme of ["light", "dark"] as const) {
  for (const vp of viewports) {
    test(`Component Showcase in ${theme} at ${vp.name} (${vp.width}x${vp.height})`, async ({ page }) => {
      await page.setViewportSize({ width: vp.width, height: vp.height });
      await page.emulateMedia({ colorScheme: theme });

      // Navigate to the design system showcase
      await page.goto("/design-system");

      // Verify core showcase hierarchy
      await expect(page.getByText("TransactX Component Showcase & Design Architecture")).toBeVisible();
      await expect(page.getByText("SIMULATED ENVIRONMENT").first()).toBeVisible();

      // Ensure theme is set correctly
      await page.evaluate((t) => {
        document.documentElement.dataset.theme = t;
        if (t === "dark") document.documentElement.classList.add("dark");
        else document.documentElement.classList.remove("dark");
      }, theme);

      // Verify buttons and interactive triggers render correctly
      await expect(page.getByRole("button", { name: "Primary Action" })).toBeVisible();
      await expect(page.getByRole("button", { name: "Send Payment" })).toBeVisible();
      await expect(page.getByRole("button", { name: "Disabled Primary" })).toBeDisabled();

      // Verify form controls
      const recipientInput = page.getByLabel("Recipient Payment Identifier");
      await expect(recipientInput).toBeVisible();
      await expect(recipientInput).toHaveValue("store@transactx");

      // Test Dialog modal interaction
      await page.getByRole("button", { name: "Launch Confirmation Modal" }).click();
      await expect(page.getByRole("dialog")).toBeVisible();
      await expect(page.getByText("Confirm Outbox Sweep")).toBeVisible();
      await page.getByRole("button", { name: "Cancel" }).click();
      await expect(page.getByRole("dialog")).not.toBeVisible();

      // Test Drawer side-panel interaction
      await page.getByRole("button", { name: "Open Receipt Drawer" }).click();
      await expect(page.getByText("Proof of Payment")).toBeVisible();
      await expect(page.getByText("Export Evidence Bundle (JSON)")).toBeVisible();
      await page.getByRole("button", { name: "Close drawer" }).click();

      // Test Tabs switching
      await page.getByRole("tab", { name: "Routing" }).click();
      await expect(page.getByText("Adaptive routing dynamically distributes 50/50")).toBeVisible();

      // Horizontal overflow check (responsive integrity)
      const hasHorizontalScroll = await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 2
      );
      expect(hasHorizontalScroll).toBe(false);

      // Run automated accessibility audit with Axe
      const scan = await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
        .analyze();

      const seriousOrCritical = scan.violations.filter(
        (v) => v.impact === "serious" || v.impact === "critical"
      );
      expect(seriousOrCritical).toEqual([]);

      // Capture screenshot preview for Visual QA
      await page.screenshot({
        path: resolve(screenshotDir, `showcase-${theme}-${vp.name}.png`),
        fullPage: true,
      });
    });
  }
}
