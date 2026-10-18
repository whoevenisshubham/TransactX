import { test,expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { createHash } from "node:crypto";

const code="c".repeat(43);
const headers={"access-control-allow-origin":"http://127.0.0.1:4173","access-control-allow-headers":"Content-Type, Authorization, X-Request-ID","access-control-allow-methods":"GET, POST, OPTIONS"};
for(const theme of ["light","dark"] as const){
 test(`Google application exchange and zero-balance welcome in ${theme}`,async({page})=>{
  await page.setViewportSize({width:theme==="dark"?390:1440,height:900});
  await page.emulateMedia({colorScheme:theme});
  let challenge="";let exchanges=0;
  await page.route("http://localhost:8080/**",async route=>{
   const request=route.request();const url=new URL(request.url());
   if(request.method()==="OPTIONS")return route.fulfill({status:204,headers});
   if(url.pathname==="/api/auth/oidc/google/start"){
    challenge=url.searchParams.get("app_challenge")??"";
    expect(challenge).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect([...url.searchParams.keys()]).toEqual(["app_challenge"]);
    return route.fulfill({status:302,headers:{location:`http://127.0.0.1:4173/auth/callback?code=${code}`}});
   }
   let data:unknown;
   if(url.pathname==="/api/auth/options")data={demoEnabled:true,registrationEnabled:false,googleEnabled:true};
   else if(url.pathname==="/api/auth/oidc/exchange"){
    exchanges++;
    const body=request.postDataJSON();
    expect(body.code).toBe(code);
    expect(createHash("sha256").update(body.verifier).digest("base64url")).toBe(challenge);
    expect(Object.keys(body).sort()).toEqual(["code","verifier"]);
    expect(page.url()).toBe("http://127.0.0.1:4173/auth/callback");
    data={token:"fixture-google-jwt",newAccount:true,user:{id:"visitor-fixture",name:"Visitor abcdef01",paymentIdentifier:"visitor-fixture@transactx",role:"CUSTOMER"}};
   } else if(url.pathname==="/api/me")data={id:"visitor-fixture",name:"Visitor abcdef01",paymentIdentifier:"visitor-fixture@transactx",role:"CUSTOMER"};
   else if(url.pathname==="/api/accounts")data=[{accountNumber:"TX-FIXTURE",balancePaise:0,status:"ACTIVE",bankCode:"BANK-A",bankName:"Example Bank"}];
   else if(url.pathname==="/api/payments")data=[];
   else throw new Error(`unexpected fixture request ${url.pathname}`);
   return route.fulfill({headers,contentType:"application/json",body:JSON.stringify({requestId:"fixture",data})});
  });
  await page.goto("/login");
  await page.getByRole("button",{name:"Continue with Google"}).click();
  await expect(page.getByRole("heading",{name:"Your private sign-in is ready."})).toBeVisible();
  await expect(page.getByText(/This simulated account starts at zero/)).toBeVisible();
  expect(await page.evaluate(()=>sessionStorage.getItem("transactx.oidc.exchange"))).toBeNull();
  expect(await page.evaluate(()=>sessionStorage.getItem("transactx.token"))).toBeNull();
  const scan=await new AxeBuilder({page}).withTags(["wcag2a","wcag2aa","wcag21a","wcag21aa"]).analyze();
  expect(scan.violations.filter(v=>v.impact==="serious"||v.impact==="critical").map(v=>v.id)).toEqual([]);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1)).toBe(true);
  if(process.env.TX_VISUAL_DIR)await page.screenshot({path:`${process.env.TX_VISUAL_DIR}/v3-google-welcome-${theme}.png`,fullPage:true});
  await page.getByRole("button",{name:"Open my private account"}).click();
  await expect(page.getByRole("heading",{name:"Good day, Visitor"})).toBeVisible();
  await expect(page.getByLabel("Account balance")).toContainText("₹0.00");
  expect(exchanges).toBe(1);
 });
}
test("Google denial and missing tab verifier never issue an exchange",async({page})=>{
 let exchanges=0;
 await page.route("**/api/**",route=>{exchanges++;return route.abort();});
 for(const suffix of ["error=denied",`code=${code}`]){
  await page.goto(`/auth/callback?${suffix}`);
  await expect(page.getByRole("heading",{name:"Let’s try another way."})).toBeVisible();
  await expect(page.getByRole("link",{name:"Explore shared demos",exact:true})).toBeVisible();
  expect(page.url()).toBe("http://127.0.0.1:4173/auth/callback");
 }
 expect(exchanges).toBe(0);
});
