import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

for (const theme of ["light","dark"] as const) for (const width of [1440,390,320]) {
  test(`isolated scenario library in ${theme} at ${width}`,async({page})=>{
    const apiRequests:string[]=[];
    // An existing session must not leak a live payment/report context into this route.
    await page.addInitScript(()=>sessionStorage.setItem("transactx.token","fixture-session-do-not-use"));
    await page.route("**/api/**",route=>{apiRequests.push(route.request().url());return route.abort();});
    await page.setViewportSize({width,height:width===1440?900:844});
    await page.emulateMedia({colorScheme:theme});
    await page.goto("/showcase?paymentId=live-context-must-not-load");
    await expect(page.getByRole("heading",{name:"Follow the difficult moments."})).toBeVisible();
    await expect(page.getByText("Illustrative scenario — not the live network",{exact:true})).toBeVisible();
    const stories=page.getByRole("navigation",{name:"Illustrative stories"}).getByRole("button");
    for(let index=0;index<8;index++) {
      await stories.nth(index).focus();
      await stories.nth(index).press("Enter");
      await expect(stories.nth(index)).toBeFocused();
      await expect(stories.nth(index)).toHaveAttribute("aria-pressed","true");
      await expect(page.getByText("Prewritten example · Not a Gemini response",{exact:true})).toBeVisible();
      const scan=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21a","wcag21aa"]).analyze();
      expect(scan.violations.filter(v=>v.impact==="serious"||v.impact==="critical").map(v=>({id:v.id,nodes:v.nodes.map(n=>({target:n.target,summary:n.failureSummary}))}))).toEqual([]);
      expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
      if(process.env.TX_VISUAL_DIR && [0,5,7].includes(index)) {await page.evaluate(()=>scrollTo(0,0));await page.screenshot({path:`${process.env.TX_VISUAL_DIR}/v3-showcase-${index+1}-${theme}-${width}.png`,fullPage:true});}
    }
    await page.getByRole("button",{name:"Next examples"}).click();
    await page.getByRole("button",{name:"Inspect example 9",exact:true}).click();
    await expect(page.getByText("Selected example · EXAMPLE 009",{exact:true})).toBeVisible();
    const citation=page.getByRole("navigation",{name:"Example evidence citations"}).getByRole("link").last();
    const target=await citation.getAttribute("href");
    await citation.click();
    await expect(page.locator(target!)).toBeFocused();
    await expect(page.getByRole("button",{name:"Next examples"})).toBeEnabled();
    expect(apiRequests).toEqual([]);
    expect(page.url()).not.toContain("live-context");
    expect(await page.evaluate(()=>sessionStorage.getItem("transactx.token"))).toBe("fixture-session-do-not-use");
  });
}
