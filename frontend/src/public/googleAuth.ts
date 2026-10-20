import { apiBaseUrl } from "../config";
import { apiRequest } from "../api";
import type { User } from "../types";

const flowKey = "transactx.oidc.exchange";
const base64url = (bytes: Uint8Array) => btoa(String.fromCharCode(...bytes)).replace(/\+/g,"-").replace(/\//g,"_").replace(/=+$/,"");

export async function beginGoogleSignIn() {
  const verifier=base64url(crypto.getRandomValues(new Uint8Array(32)));
  const challenge=base64url(new Uint8Array(await crypto.subtle.digest("SHA-256",new TextEncoder().encode(verifier))));
  // Storage must succeed before redirect. This is distinct from server PKCE.
  sessionStorage.setItem(flowKey,JSON.stringify({verifier,createdAt:Date.now()}));
  window.location.assign(`${apiBaseUrl}/api/auth/oidc/google/start?app_challenge=${encodeURIComponent(challenge)}`);
}

type ExchangeResult={token:string;user:User;newAccount:boolean};
let exchange:Promise<ExchangeResult>|undefined;
export function completeGoogleSignIn():Promise<ExchangeResult> {
  if(exchange) return exchange;
  const query=new URLSearchParams(window.location.search);
  // Remove the opaque code before any asynchronous work or navigation.
  window.history.replaceState({},"","/auth/callback");
  exchange=(async()=>{
    const stored=sessionStorage.getItem(flowKey);
    sessionStorage.removeItem(flowKey);
    if(query.has("error")) throw new Error(query.get("error")==="denied" ? "Google sign-in was cancelled. You can try again or explore a shared demo." : "Google sign-in could not be completed. Start again or explore a shared demo.");
    const code=query.get("code");
    if(!code || query.getAll("code").length!==1 || !/^[A-Za-z0-9_-]{43}$/.test(code) || !stored) throw new Error("This sign-in has expired or belongs to another browser tab. Start again.");
    const flow=JSON.parse(stored) as {verifier?:string;createdAt?:number};
    if(typeof flow.verifier!=="string" || !/^[A-Za-z0-9_-]{43}$/.test(flow.verifier) || typeof flow.createdAt!=="number" || Date.now()-flow.createdAt>10*60*1000 || flow.createdAt>Date.now()+60000) throw new Error("This sign-in has expired. Start again.");
    return apiRequest<ExchangeResult>("/api/auth/oidc/exchange",{method:"POST",body:JSON.stringify({code,verifier:flow.verifier})});
  })();
  // One promise also prevents React StrictMode from consuming the code twice.
  return exchange;
}
