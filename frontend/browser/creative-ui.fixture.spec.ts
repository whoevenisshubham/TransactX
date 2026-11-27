import { expect, test } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("landing interactive simulation, blueprint modal, audio opt-in, and command palette work cleanly", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.setViewportSize({ width: 1440, height: 900 });

  const apiRequests: string[] = [];
  page.on("request", (req) => {
    if (req.url().includes("/api/")) apiRequests.push(req.url());
  });

  await page.goto("/");

  // Web Audio sound toggle is strictly OFF by default and toggles intentionally
  const soundBtn = page.getByRole("button", { name: "Enable interactive sound effects" }).first();
  await expect(soundBtn).toHaveAttribute("aria-pressed", "false");
  await soundBtn.click();
  await expect(page.getByRole("button", { name: "Mute interactive sound effects" }).first()).toHaveAttribute(
    "aria-pressed",
    "true"
  );

  // Drop-the-wire narrative sequence toggle + reset
  await page.getByRole("button", { name: "Play sequence" }).click();
  await expect(page.getByRole("button", { name: "Pause walkthrough" })).toBeVisible();
  await page.getByRole("button", { name: "Reset failure simulator" }).click();
  await expect(page.getByRole("button", { name: "Play sequence" })).toBeVisible();

  // Interactive blueprint modal expand, keyboard focus containment, Escape close + restore, close button, and Axe
  const expandTrigger = page.getByRole("button", { name: "Expand blueprint" });
  await expandTrigger.focus();
  await expandTrigger.click();
  const blueprintDialog = page.getByRole("dialog", { name: "Expanded system architecture blueprint explorer" });
  await expect(blueprintDialog).toBeVisible();
  await expect(page.getByRole("button", { name: "Zoom in diagram" })).toBeVisible();
  await page.getByRole("button", { name: "Zoom in diagram" }).click();
  await expect(page.getByText(/115% zoom/)).toBeVisible();

  // Verify focus is contained inside the dialog on Tab cycling
  await page.waitForFunction(() => {
    const dialog = document.querySelector('[role="dialog"][aria-label="Expanded system architecture blueprint explorer"]');
    return Boolean(dialog && dialog.contains(document.activeElement));
  });
  for (let i = 0; i < 8; i++) {
    await page.keyboard.press("Tab");
    const inside = await page.evaluate(() => {
      const dialog = document.querySelector('[role="dialog"][aria-label="Expanded system architecture blueprint explorer"]');
      return Boolean(dialog && dialog.contains(document.activeElement));
    });
    expect(inside).toBe(true);
  }

  const modalScan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(
    modalScan.violations
      .filter((v) => v.impact === "serious" || v.impact === "critical")
      .map((v) => ({ id: v.id, nodes: v.nodes.map((n) => n.target) }))
  ).toEqual([]);

  // Escape closes modal and restores focus to trigger
  await page.keyboard.press("Escape");
  await expect(blueprintDialog).toHaveCount(0);
  await expect(expandTrigger).toBeFocused();

  // Re-open and verify explicit close button works & restores focus
  await expandTrigger.click();
  await expect(blueprintDialog).toBeVisible();
  await page.getByRole("button", { name: "Close blueprint modal" }).click();
  await expect(blueprintDialog).toHaveCount(0);
  await expect(expandTrigger).toBeFocused();

  // Command palette open & Escape close
  await page.getByRole("button", { name: "Open command palette" }).first().click();
  await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "Command palette" })).toHaveCount(0);

  expect(apiRequests).toEqual([]);
});

test("/demo surfaces clear actionable guidance on HTML rewrite 405 and gateway 502 wakeup", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });

  await page.route("**/api/auth/demo", async (route) => {
    if (route.request().method() === "OPTIONS") {
      return route.fulfill({
        status: 204,
        headers: {
          "access-control-allow-origin": "http://127.0.0.1:4173",
          "access-control-allow-headers": "Content-Type",
          "access-control-allow-methods": "POST, OPTIONS",
        },
      });
    }
    return route.fulfill({
      status: 405,
      contentType: "text/html",
      body: "<!doctype html><html><body>Method Not Allowed</body></html>",
    });
  });

  await page.goto("/demo");
  await page.getByRole("button", { name: "Explore customer" }).click();
  await expect(page.getByRole("alert")).toContainText("VITE_API_BASE_URL");
  await expect(page.getByRole("button", { name: "Retry", exact: true })).toBeVisible();

  // Switch backend failure to 502 gateway idle wakeup and trigger Retry
  await page.unroute("**/api/auth/demo");
  await page.route("**/api/auth/demo", async (route) => {
    if (route.request().method() === "OPTIONS") {
      return route.fulfill({
        status: 204,
        headers: {
          "access-control-allow-origin": "http://127.0.0.1:4173",
          "access-control-allow-headers": "Content-Type",
          "access-control-allow-methods": "POST, OPTIONS",
        },
      });
    }
    return route.fulfill({
      status: 502,
      contentType: "text/plain",
      body: "Bad Gateway",
    });
  });

  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("waking up from idle");

  const scan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .analyze();
  expect(
    scan.violations
      .filter((v) => v.impact === "serious" || v.impact === "critical")
      .map((v) => ({ id: v.id, nodes: v.nodes.map((n) => n.target) }))
  ).toEqual([]);
});

test("showcase playground interactive 3D prism, particle canvas, audio opt-in, and ambient controls work without autoplay", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/showcase");

  const playgroundHeading = page.getByRole("heading", {
    name: "Explore network telemetry, sound synthesis & 3D artifacts",
  });
  await playgroundHeading.scrollIntoViewIfNeeded();
  await expect(playgroundHeading).toBeVisible();

  // Audio strictly OFF by default (no autoplay)
  const sfxButton = page.getByRole("button", { name: "Enable SFX" });
  const ambientButton = page.getByRole("button", { name: "Start Ambient Fintech Drone" });
  await expect(sfxButton).toHaveAttribute("aria-pressed", "false");
  await expect(ambientButton).toHaveAttribute("aria-pressed", "false");

  // Explicit sound opt-in and mute toggle
  await sfxButton.click();
  const muteButton = page.getByRole("button", { name: "Mute SFX" });
  await expect(muteButton).toHaveAttribute("aria-pressed", "true");
  await muteButton.click();
  await expect(page.getByRole("button", { name: "Enable SFX" })).toHaveAttribute("aria-pressed", "false");

  // Explicit ambient synthscape start and stop
  await ambientButton.click();
  const stopAmbientButton = page.getByRole("button", { name: "Stop Ambient Synthscape" });
  await expect(stopAmbientButton).toHaveAttribute("aria-pressed", "true");
  await stopAmbientButton.click();
  await expect(page.getByRole("button", { name: "Start Ambient Fintech Drone" })).toHaveAttribute("aria-pressed", "false");

  // Interactive NetworkCanvas particle/rail mode & click interaction
  const wireDropMode = page.getByRole("button", { name: "wire-drop", exact: true });
  await wireDropMode.click();
  const canvas = page.getByLabel("Interactive two-bank simulated payment network topology canvas");
  await expect(canvas).toBeVisible();
  const box = await canvas.boundingBox();
  expect(box).not.toBeNull();
  if (box) {
    // Click near Bank A node (nx: 0.16, ny: 0.52) to trigger node selection + pulse ripple
    await page.mouse.click(box.x + box.width * 0.16, box.y + box.height * 0.52);
  }
  await expect(page.getByText(/Selected:/)).toContainText("bank-a");

  // Interactive 3D vault prism drag + reset
  const prismCard = page.getByTestId("vault-prism-card");
  const prismStage = page.getByTestId("vault-prism-stage");
  await prismStage.scrollIntoViewIfNeeded();
  const initialTransform = await prismCard.evaluate((el) => el.style.transform);
  const stageBox = await prismStage.boundingBox();
  expect(stageBox).not.toBeNull();
  if (stageBox) {
    await page.mouse.move(stageBox.x + stageBox.width * 0.5, stageBox.y + stageBox.height * 0.5);
    await page.mouse.down();
    await page.mouse.move(stageBox.x + stageBox.width * 0.82, stageBox.y + stageBox.height * 0.28, {
      steps: 5,
    });
    await page.mouse.up();
  }
  await expect
    .poll(() => prismCard.evaluate((el) => el.style.transform))
    .not.toBe(initialTransform);

  await page.getByRole("button", { name: "Reset 3D orientation" }).click();
  await expect(prismCard).toHaveCSS("transform", /.*/);
  const resetTransform = await prismCard.evaluate((el) => el.style.transform);
  expect(resetTransform).toBe(initialTransform);
});
