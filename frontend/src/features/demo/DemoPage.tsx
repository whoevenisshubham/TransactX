import React, { useEffect, useState, useRef } from "react";
import { authClient, type DemoPersona } from "../../public/authClient";
import { PublicHeader } from "../public/PublicHeader";
import { Button } from "../../components/ui/button";
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from "../../components/ui/card";
import { Badge } from "../../components/ui/badge";
import { ArrowRight, Send, QrCode, Activity, ShieldAlert, AlertCircle, Clock, X, RefreshCw } from "lucide-react";

interface PersonaConfig {
  key: DemoPersona;
  title: string;
  name: string;
  description: string;
  roleScope: string;
  icon: React.ReactNode;
}

const personas: PersonaConfig[] = [
  {
    key: "customer",
    title: "Customer",
    name: "Alice · Bank A",
    description: "Send a payment, review your balance, view a payment receipt and inspect the available transaction evidence.",
    roleScope: "Consumer Participant · Bank A",
    icon: <Send className="w-5 h-5 text-[var(--action-accent-bg)]" />,
  },
  {
    key: "merchant",
    title: "Merchant",
    name: "Demo Store · Bank B",
    description: "Share your QR, review recent incoming payments and refresh to fetch current observations.",
    roleScope: "Merchant Receiving Station · Bank B",
    icon: <QrCode className="w-5 h-5 text-[var(--action-accent-bg)]" />,
  },
  {
    key: "operator",
    title: "Operator",
    name: "Read-only viewer",
    description: "Follow payment journeys, compare bank evidence and investigate ambiguous outcomes.",
    roleScope: "Forensic View · Read-Only (OPS_VIEWER)",
    icon: <Activity className="w-5 h-5 text-[var(--action-accent-bg)]" />,
  },
];

export function DemoPage({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const requested = new URLSearchParams(window.location.search).get("role");
  const [busy, setBusy] = useState<DemoPersona | null>(null);
  const [error, setError] = useState("");
  const [elapsed, setElapsed] = useState(0);
  const abortControllerRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!busy) return;
    const started = Date.now();
    const timer = window.setInterval(
      () => setElapsed(Math.floor((Date.now() - started) / 1000)),
      1000
    );
    return () => window.clearInterval(timer);
  }, [busy]);

  async function enter(persona: DemoPersona) {
    if (busy) return;
    setBusy(persona);
    setError("");
    setElapsed(0);

    const controller = new AbortController();
    abortControllerRef.current = controller;

    try {
      const response = await authClient.demo(persona);
      onAuthenticated(response.token);
    } catch (failure) {
      if (controller.signal.aborted) {
        setError("Sign-in cancelled. The request was aborted.");
      } else {
        setError(
          failure instanceof Error
            ? failure.message
            : "Demo access is unavailable. Please try again."
        );
      }
    } finally {
      setBusy(null);
      abortControllerRef.current = null;
    }
  }

  function handleCancel() {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    setBusy(null);
  }

  return (
    <div className="public-site reset-public reset-access min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors">
      <PublicHeader />

      <main className="public-container demo-page flex-1 w-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-10 sm:py-14 space-y-10">
        {/* Page Heading */}
        <div className="space-y-3 max-w-2xl">
          <p className="eyebrow text-xs font-mono font-semibold uppercase tracking-wider text-[var(--action-accent-bg)]">
            Explore the live simulated network
          </p>
          <h1 className="text-3xl sm:text-4xl font-extrabold tracking-tight text-[var(--text-primary)]">
            One system.<br />
            <span className="text-[var(--text-secondary)]">Choose your perspective.</span>
          </h1>
          <p className="demo-lead text-sm text-[var(--text-secondary)] leading-relaxed">
            No registration needed. These shared synthetic accounts use simulated money. Their activity is visible to other demo visitors.
          </p>
        </div>

        {/* Demo Notice Banner */}
        <div className="demo-notice p-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] text-xs flex items-start gap-3 shadow-sm">
          <span className="text-[var(--action-accent-bg)] font-bold text-sm" aria-hidden="true">
            ↳
          </span>
          <p className="text-[var(--text-secondary)] leading-relaxed">
            The free backend sleeps between visits. Your first sign-in may take longer while it wakes.{" "}
            <strong className="text-[var(--text-primary)]">
              Public demo access never grants administrator controls.
            </strong>{" "}
            Operator access is restricted to read-only viewing (<code className="font-mono text-[11px]">OPS_VIEWER</code>).
          </p>
        </div>

        {/* Error Notification */}
        {error && (
          <div
            role="alert"
            className="p-4 rounded-xl border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-xs text-[var(--status-danger-fg)] flex items-start justify-between gap-3 shadow-sm"
          >
            <div className="flex items-start gap-2.5">
              <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
              <div>
                <p className="font-semibold">{error}</p>
                <p className="mt-1 opacity-90">
                  You can retry the connection or sign in with private credentials.
                </p>
              </div>
            </div>
            {busy === null && (
              <button
                type="button"
                onClick={() => setError("")}
                className="text-[var(--status-danger-fg)] hover:opacity-75 cursor-pointer text-xs font-mono"
              >
                Dismiss
              </button>
            )}
          </div>
        )}

        {/* 3 Primary Persona Cards */}
        <div className="role-cards grid grid-cols-1 md:grid-cols-3 gap-6">
          {personas.map((persona, index) => {
            const isRecommended = requested === persona.key;
            const isThisBusy = busy === persona.key;

            return (
              <section
                key={persona.key}
                className={`role-card demo-role flex flex-col justify-between p-6 rounded-2xl border transition-all ${
                  isRecommended
                    ? "is-recommended border-[var(--action-accent-bg)] bg-[var(--bg-surface)] shadow-md ring-2 ring-[var(--action-accent-bg)]/20"
                    : "border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm hover:shadow-md"
                }`}
              >
                <div className="space-y-4">
                  <div className="role-card-top flex items-center justify-between">
                    <span className="role-icon p-2.5 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]">
                      {persona.icon}
                    </span>
                    <span className="role-number font-mono text-xs font-bold text-[var(--text-tertiary)]">
                      0{index + 1}
                    </span>
                  </div>

                  <div>
                    <span className="role-name text-[11px] font-mono text-[var(--text-tertiary)] block">
                      {persona.name}
                    </span>
                    <h2 className="text-xl font-bold tracking-tight text-[var(--text-primary)] mt-0.5">
                      {persona.title}
                    </h2>
                  </div>

                  <p className="text-xs text-[var(--text-secondary)] leading-relaxed">
                    {persona.description}
                  </p>

                  <div className="pt-1">
                    <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)] text-[var(--text-tertiary)]">
                      {persona.roleScope}
                    </span>
                  </div>
                </div>

                <div className="pt-6">
                  <Button
                    variant="primary"
                    className="w-full justify-center gap-2"
                    disabled={busy !== null}
                    loading={isThisBusy}
                    onClick={() => void enter(persona.key)}
                  >
                    <span>
                      {isThisBusy ? "Connecting…" : `Explore ${persona.title.toLowerCase()}`}
                    </span>
                    {!isThisBusy && <ArrowRight className="w-3.5 h-3.5" />}
                  </Button>
                </div>
              </section>
            );
          })}
        </div>

        {/* Live Elapsed Progress & Cancellation */}
        <div className="flex flex-col sm:flex-row items-center justify-between gap-4 p-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/40 text-xs">
          <p className="demo-progress text-xs font-mono text-[var(--text-secondary)]" role="status">
            {busy ? (
              <span className="flex items-center gap-2">
                <Clock className="w-3.5 h-3.5 text-[var(--action-accent-bg)] animate-spin" />
                <span>
                  {elapsed < 8
                    ? "Connecting to the demo"
                    : "Still waiting for the demo service"}{" "}
                  · {elapsed}s elapsed. Please keep this page open.
                </span>
              </span>
            ) : (
              "Shared demo accounts are separate from private sign-in."
            )}
          </p>

          {busy !== null && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              className="text-xs shrink-0 cursor-pointer"
            >
              <X className="w-3 h-3 mr-1" />
              <span>Cancel wait</span>
            </Button>
          )}
        </div>

        {/* Secondary Recipient Option & Private Sign In */}
        <div className="demo-secondary p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] flex flex-col sm:flex-row items-center justify-between gap-4">
          <div>
            <h3 className="text-sm font-semibold text-[var(--text-primary)]">
              Need the other side of a transfer?
            </h3>
            <p className="text-xs text-[var(--text-secondary)] mt-0.5">
              Launch Bob's receiving account at Bank B to watch transfers arrive.
            </p>
          </div>

          <div className="flex items-center gap-3">
            <Button
              variant="secondary"
              disabled={busy !== null}
              loading={busy === "recipient"}
              onClick={() => void enter("recipient")}
            >
              <span>{busy === "recipient" ? "Connecting…" : "Open Bob · Bank B"}</span>
            </Button>
            <a
              href="/login"
              className="text-xs font-semibold text-[var(--action-accent-bg)] hover:underline inline-flex items-center gap-1"
            >
              <span>Use your own account</span>
              <span aria-hidden="true">↗</span>
            </a>
          </div>
        </div>
      </main>
    </div>
  );
}
