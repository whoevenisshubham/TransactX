import { useEffect, useState } from "react";
import { Button, InlineError } from "../components";
import { PublicHeader } from "./LandingPage";
import { completeGoogleSignIn } from "./googleAuth";

export function GoogleCallbackPage({onAuthenticated}:{onAuthenticated:(token:string)=>void}) {
  const [error,setError]=useState("");
  const [newToken,setNewToken]=useState("");
  useEffect(()=>{
    let active=true;
    completeGoogleSignIn().then(result=>{
      if(!active)return;
      if(result.newAccount) setNewToken(result.token);
      else { window.history.replaceState({},"","/");onAuthenticated(result.token); }
    }).catch(()=>{if(active)setError("Google sign-in could not be completed or was cancelled. Start again, or explore a shared demo.");});
    return()=>{active=false;};
  },[onAuthenticated]);
  return <div className="public-site reset-public reset-access"><PublicHeader/><main className="google-callback public-container"><p className="eyebrow">Private account access</p><h1>{newToken?"Your private sign-in is ready.":error?"Let’s try another way.":"Verifying your sign-in…"}</h1>{newToken?<><p>This simulated account starts at zero. Explore a funded shared demo to try payments. Your Google email and real name are not used as your public identity.</p><Button onClick={()=>{window.history.replaceState({},"","/");onAuthenticated(newToken);}}>Open my private account</Button><a className="button button-secondary" href="/demo">Explore funded shared demos</a></>:error?<><InlineError message={error}/><a className="button button-primary" href="/login">Try sign-in again</a><a className="button button-secondary" href="/demo">Explore shared demos</a></>:<p role="status">Exchanging a one-time code securely. No Google tokens are stored in your browser.</p>}</main></div>;
}
