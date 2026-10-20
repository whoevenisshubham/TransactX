import { useEffect, useState } from "react";
import { Button, Icon, InlineError } from "../components";
import { authClient, type DemoPersona } from "./authClient";
import { PublicHeader } from "./LandingPage";

const personas: { key: DemoPersona; title: string; name: string; description: string; icon: "send" | "qr" | "activity" }[] = [
  { key: "customer", title: "Customer", name: "Alice · Bank A", description: "Send a payment, review your balance and open a receipt.", icon: "send" },
  { key: "merchant", title: "Merchant", name: "Demo Store · Bank B", description: "Share your QR, inspect incoming payments and reconcile their status.", icon: "qr" },
  { key: "operator", title: "Operator", name: "Read-only viewer", description: "Follow payment journeys, compare bank evidence and investigate outcomes.", icon: "activity" },
];

export function DemoPage({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const requested = new URLSearchParams(location.search).get("role");
  const [busy, setBusy] = useState<DemoPersona | null>(null);
  const [error, setError] = useState("");
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    if (!busy) return;
    const started = Date.now();
    const timer = window.setInterval(() => setElapsed(Math.floor((Date.now() - started) / 1000)), 1000);
    return () => window.clearInterval(timer);
  }, [busy]);
  async function enter(persona: DemoPersona) {
    if (busy) return;
    setBusy(persona); setError(""); setElapsed(0);
    try { onAuthenticated((await authClient.demo(persona)).token); }
    catch (failure) { setError(failure instanceof Error ? failure.message : "Demo access is unavailable. Please try again."); }
    finally { setBusy(null); }
  }
  return <div className="public-site reset-public reset-access"><PublicHeader /><main className="public-container demo-page"><p className="eyebrow">Explore the live simulated network</p><h1>One system.<br /><span>Choose your perspective.</span></h1><p className="demo-lead">No registration needed. These shared synthetic accounts use simulated money. Their activity is visible to other demo visitors.</p><div className="demo-notice"><span aria-hidden="true">↳</span><p>The free backend sleeps between visits. Your first sign-in may take longer while it wakes. Public demo access never grants administrator controls.</p></div>{error && <InlineError message={error} />}<div className="role-cards">{personas.map((persona, index) => <section className={`role-card demo-role ${requested === persona.key ? "is-recommended" : ""}`} key={persona.key}><div className="role-card-top"><span className="role-icon"><Icon name={persona.icon} size={22} /></span><span className="role-number">0{index + 1}</span></div><p className="role-name">{persona.name}</p><h2>{persona.title}</h2><p>{persona.description}</p><Button disabled={busy !== null} onClick={() => void enter(persona.key)}>{busy === persona.key ? "Connecting…" : `Explore ${persona.title.toLowerCase()}`}<Icon name="arrow" /></Button></section>)}</div><p className="demo-progress" role="status">{busy ? `${elapsed < 8 ? "Connecting to the demo" : "Waking the demo backend"} · ${elapsed}s elapsed. Please keep this page open.` : "Shared demo accounts are separate from private sign-in."}</p><div className="demo-secondary"><p>Need the other side of a transfer?</p><Button variant="secondary" disabled={busy !== null} onClick={() => void enter("recipient")}>{busy === "recipient" ? "Connecting…" : "Open Bob · Bank B"}</Button><a href="/login">Use your own account <span aria-hidden="true">↗</span></a></div></main></div>;
}
