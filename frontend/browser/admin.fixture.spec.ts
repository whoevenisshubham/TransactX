import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { adminFixtures, participant, operationId, start, end } from "./support/adminFixtures";

for (const theme of ["light", "dark"] as const) for (const width of [1440,390]) {
  test(`private operator sources and verification screens in ${theme} at ${width}`,async({page}) => {
    const childScopes:(string|null)[]=[];
    await adminFixtures(page,id=>childScopes.push(id));
    await page.setViewportSize({width,height:width===1440?900:844});
    await page.emulateMedia({colorScheme:theme});
    async function inspect(screen:string) {
      const scan=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21a","wcag21aa"]).analyze();
      expect(scan.violations.filter(item=>item.impact==="serious"||item.impact==="critical").map(item=>({id:item.id,nodes:item.nodes.map(node=>({target:node.target,summary:node.failureSummary}))}))).toEqual([]);
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
      await page.evaluate(()=>window.scrollTo({top:0,behavior:"instant"}));
      if(process.env.TX_VISUAL_DIR) await page.screenshot({path:`${process.env.TX_VISUAL_DIR}/v3-admin-${screen}-${theme}-${width}.png`,fullPage:true});
    }
    await page.goto("/console");
    await expect(page.getByText(/Last fetch finished/)).toBeVisible();
    await expect(page.getByText("Not evaluated",{exact:true})).toBeVisible();
    await expect(page.getByText("Database health endpoint").locator("..")).toContainText("Unavailable");
    await inspect("overview");
    await page.goto("/console/health");
    await expect(page.getByRole("heading",{name:"Bank health",exact:true})).toBeVisible();
    await page.getByLabel("Execution target",{exact:true}).selectOption("RAIL-B");
    await expect(page.getByText("Not evaluated",{exact:true})).toBeVisible();
    await page.getByRole("button",{name:"Collect one probe"}).click();
    await expect(page.getByRole("heading",{name:"success",exact:true})).toBeVisible();
    await inspect("health");
    await page.goto("/console/routing");
    await expect(page.getByRole("heading",{name:"Target Circuit Controls"})).toBeVisible();
    await expect(page.getByText("RAIL-A",{exact:true}).first()).toBeVisible();
    await inspect("routing");
    await page.goto("/console/reconciliation");
    await expect(page.getByRole("heading",{name:"Reconciliation Runs"})).toBeVisible();
    await page.getByRole("button",{name:"View",exact:true}).click();
    await expect(page.getByText("No discrepancy evidence in this page.")).toBeVisible();
    await inspect("reconciliation");
    await page.goto("/console/merkle");
    await page.getByLabel("Participant ID",{exact:true}).fill(participant);
    await page.getByLabel("Scope From (ISO8601)").fill(start);
    await page.getByLabel("Scope To (ISO8601)").fill(end);
    await page.getByRole("button",{name:/Fetch.*Root/}).click();
    await expect(page.getByRole("button",{name:"Load Children"})).toBeVisible();
    await page.getByLabel("Participant ID",{exact:true}).fill("dddddddd-dddd-4ddd-8ddd-dddddddddddd");
    await page.getByRole("button",{name:"Load Children"}).click();
    await expect(page.getByRole("button",{name:"Expand node"})).toBeVisible();
    expect(childScopes).toEqual([participant]);
    await inspect("merkle");
    await page.goto("/console/integrity");
    await page.getByLabel("Operation ID",{exact:true}).fill(operationId);
    await page.getByLabel("Participant ID",{exact:true}).fill(participant);
    await page.getByLabel("Scope From (ISO8601)").fill(start);
    await page.getByLabel("Scope To (ISO8601)").fill(end);
    await page.getByRole("button",{name:"Fetch Proof",exact:true}).click();
    await page.getByRole("button",{name:"Verify Proof Against Trusted State"}).click();
    await expect(page.getByRole("heading",{name:"VERIFICATION FAILED"})).toBeVisible();
    await inspect("integrity");
    await page.goto("/console/chaos");
    await expect(page.getByRole("heading",{name:"Chaos Simulation Controller"})).toBeVisible();
    await inspect("controlled-faults");
    await page.goto("/console/activity");
    await expect(page.getByText("Browser fixture circuit observation")).toBeVisible();
    await inspect("activity");
  });
}
