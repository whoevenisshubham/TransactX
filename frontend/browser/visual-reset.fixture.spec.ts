import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { installFixtureApi, paymentId, investigationId, fulfillFixture, fixture } from "./support/opsFixtures";

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
          else if (path === "/api/payments") data = (merchant ? ["Alice Demo", "Bob Demo", "Asha Demo", "Kiran Demo", "Mira Demo"] : ["Studio Store", "Bob Demo", "Asha Demo", "Studio Store", "Bob Demo"]).map((name,index) => ({ id: `${index + 1}1111111-1111-4111-8111-111111111111`, amountPaise: [12500,24000,8750,18000,3200][index], currency: "INR", state: index === 2 ? "PENDING_RECONCILIATION" : "COMPLETED", note: "Synthetic design fixture", createdAt: `2026-10-19T${String(12-index).padStart(2,"0")}:10:00Z`, senderName: merchant || index === 2 ? name : "Alice Fixture", senderPaymentIdentifier: merchant || index === 2 ? `${name.split(" ")[0].toLowerCase()}@fixture` : identifier, receiverName: merchant ? "Studio Store Fixture" : index === 2 ? "Alice Fixture" : name, receiverPaymentIdentifier: merchant || index === 2 ? identifier : `${name.split(" ")[0].toLowerCase()}@fixture`, direction: merchant ? "RECEIVED" : index === 2 ? "RECEIVED" : "SENT", sourceBankCode: index === 2 && !merchant ? "BANK-B" : "BANK-A", destinationBankCode: index === 2 && !merchant ? "BANK-A" : "BANK-B" }));
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
      if (process.env.TX_VISUAL_DIR && (width === 1440 || (screen === "customer" && width === 390)) && screen !== "landing") {
        const subject = screen === "customer" ? ".personal-account-document" : screen === "merchant" ? ".merchant-receive-station" : ".journey-rail";
        await page.locator(subject).screenshot({path:`${process.env.TX_VISUAL_DIR}/preview-${screen}-${theme}.png`});
      }
      if (process.env.TX_VISUAL_DIR && screen === "journey") {
        await page.getByRole("link",{name:"Detailed evidence ↓"}).click();
        await page.evaluate(()=>scrollTo(0,0));
        await page.screenshot({path:`${process.env.TX_VISUAL_DIR}/expanded-${screen}-${theme}-${width}.png`,fullPage:true});
        await page.locator("#journey-detailed-evidence>summary").click();
      }

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


for (const width of [1440,390,320]) test(`disclosed evidence citation reveals exact operation and deep link at ${width}`, async ({page}) => {
  await page.setViewportSize({width,height:844});
  await page.emulateMedia({reducedMotion:"reduce"});
  await installFixtureApi(page,"OPS_VIEWER");
  const operation = fixture("evidence-partial.json").data.operations[0];
  await page.route(`http://localhost:8080/api/ops/investigations/${investigationId}`, async route => {
    if (route.request().method() !== "GET") return route.fallback();
    const data=fixture("investigation-evidence-only.json").data;
    data.report.facts=[{text:`The original destination operation is ${operation.operationId}.`,evidenceIds:[operation.evidenceId],assertions:[{field:"operation_id",value:operation.operationId,entityId:operation.operationId,evidenceId:operation.evidenceId}],contradiction:false}];
    return fulfillFixture(route,data);
  });
  await page.goto(`/console/payments/${paymentId}`);
  const disclosure=page.locator("#journey-detailed-evidence");
  await expect(disclosure).not.toHaveAttribute("open");
  await page.getByRole("link",{name:"Ask about this payment ↓"}).click();
  await expect(page.getByRole("complementary",{name:"Investigation workspace"})).toBeFocused();
  await page.getByLabel("Question",{exact:true}).fill("What did the destination report?");
  await page.getByRole("button",{name:"Start investigation",exact:true}).click();
  const citation=page.getByRole("button",{name:`View evidence ${operation.evidenceId}`,exact:true});
  await citation.focus();await page.keyboard.press("Enter");
  const target=page.locator(`[id="evidence-${operation.evidenceId}"]`);
  await expect(disclosure).toHaveAttribute("open", "");
  await expect(target).toBeVisible();await expect(target).toBeFocused();
  expect(await target.evaluate(el=>{const r=el.getBoundingClientRect();return r.top>=0&&r.top<innerHeight;})).toBe(true);
  await page.goto(`/console/payments/${paymentId}#evidence-${encodeURIComponent(operation.evidenceId)}`);
  await expect(disclosure).toHaveAttribute("open", "");
  await expect(target).toBeFocused();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
});

for(const role of ["CUSTOMER","MERCHANT"] as const) for(const theme of ["light","dark"] as const) test(`readable ${role} long identity large money and empty state ${theme}`, async({page})=>{
  await page.setViewportSize({width:320,height:844});await page.emulateMedia({colorScheme:theme,reducedMotion:"reduce"});
  await installFixtureApi(page,"OPS_VIEWER");
  await page.route("http://localhost:8080/api/me",route=>route.request().method()!=="GET"?route.fallback():fulfillFixture(route,{id:"stress-fixture",name:"An exceptionally long account holder name for readable layout inspection",paymentIdentifier:"long-owner-reference-123456789012345678901234567890@fixture",role}));
  await page.route("http://localhost:8080/api/accounts",route=>route.request().method()!=="GET"?route.fallback():fulfillFixture(route,[{accountNumber:"TX-LONG-ACCOUNT-123456789012345678901234567890",bankName:"Bank A Fixture with a long descriptive identity",bankCode:"BANK-A",balancePaise:9876543210,status:"ACTIVE"}]));
  await page.route("http://localhost:8080/api/payments?**",route=>route.request().method()!=="GET"?route.fallback():fulfillFixture(route,[]));
  await page.goto(role==="MERCHANT"?"/merchant":"/");
  await expect(page.getByRole("heading",{level:1})).toBeVisible();
  await expect(page.getByText("₹98,765,432.10",{exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
  const scope=role==="CUSTOMER"?".personal-account-imprint .customer-bank-identity":".merchant-position small";
  expect(await page.locator(scope).evaluate(el=>parseFloat(getComputedStyle(el).fontSize))).toBeGreaterThanOrEqual(12);
  if(process.env.TX_VISUAL_DIR)await page.screenshot({path:`${process.env.TX_VISUAL_DIR}/stress-${role.toLowerCase()}-${theme}-320.png`,fullPage:true});
});

test("journey loading unavailable and retained stale evidence stay explicit",async({page})=>{
  await page.setViewportSize({width:390,height:844});await page.clock.install();await installFixtureApi(page,"OPS_VIEWER");
  let release:()=>void=()=>{};const barrier=new Promise<void>(resolve=>{release=resolve;});let unavailable=false;
  await page.route(`http://localhost:8080/api/ops/payments/${paymentId}/evidence`,async route=>{
    if(route.request().method()!=="GET")return route.fallback();
    if(!unavailable)await barrier;
    if(unavailable)return route.fulfill({status:503,headers:{"access-control-allow-origin":"http://127.0.0.1:4173"},contentType:"application/json",body:JSON.stringify({error:{code:"UNAVAILABLE",message:"Fixture source unavailable"}})});
    return fulfillFixture(route,fixture("evidence-partial.json").data);
  });
  await page.goto(`/console/payments/${paymentId}`);await expect(page.getByText("Loading payment evidence…")).toBeVisible();release();
  await expect(page.getByRole("heading",{name:"Independent observations"})).toBeVisible();
  unavailable=true;await page.clock.fastForward(15001);
  await expect(page.getByText("Showing the last collected evidence while refreshing.",{exact:false})).toBeVisible();
  await expect(page.getByText("An unavailable or unknown bank observation does not prove failure.",{exact:false})).toBeVisible();
  await expect(page.getByRole("link",{name:"Ask about this payment ↓"})).toBeVisible();
  if(process.env.TX_VISUAL_DIR)await page.screenshot({path:`${process.env.TX_VISUAL_DIR}/journey-stale-390.png`,fullPage:true});
  await page.reload();await expect(page.getByRole("alert")).toBeVisible();await expect(page.getByRole("complementary",{name:"Investigation workspace"})).toHaveCount(0);
});
