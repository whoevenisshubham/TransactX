import React, { useEffect, useState, useRef } from "react";
import { authClient, type DemoPersona } from "../../public/authClient";
import { PublicHeader } from "../public/PublicHeader";
import { Button } from "../../components/ui/button";
import {
  SpotlightCard,
  StaggerGroup,
  StaggerItem,
  ScrambleText,
  DotSentinel,
  useAudioEngine,
} from "../../components/effects";
import { ArrowRight, Send, QrCode, Activity, AlertCircle, Clock, X, RefreshCw } from "lucide-react";

interface PersonaConfig {
  key: DemoPersona;
  title: string;
  name: string;
  description: string;
  roleScope: string;
  accentColor: string;
  icon: React.ReactNode;
}

const personas: PersonaConfig[] = [
  {
    key: "customer",
    title: "Customer",
    name: "Alice · Bank A",
    description: "Send a payment, review your balance, view a payment receipt and inspect the available transaction evidence.",
    roleScope: "Consumer Participant · Bank A",
    accentColor: "rgba(59, 130, 246, 0.2)",
    icon: <Send className="w-5 h-5 text-[var(--blueprint-cobalt,#2563eb)]" />,
  },
  {
    key: "merchant",
    title: "Merchant",
    name: "Demo Store · Bank B",
    description: "Share your QR, review recent incoming payments and refresh to fetch current observations.",
    roleScope: "Merchant Receiving Station · Bank B",
    accentColor: "rgba(16, 185, 129, 0.2)",
    icon: <QrCode className="w-5 h-5 text-[var(--blueprint-emerald,#059669)]" />,
  },
  {
    key: "operator",
    title: "Operator",
    name: "Read-only viewer",
    description: "Follow payment journeys, compare bank evidence and investigate ambiguous outcomes.",
    roleScope: "Forensic View · Read-Only (OPS_VIEWER)",
    accentColor: "rgba(139, 92, 246, 0.2)",
    icon: <Activity className="w-5 h-5 text-[var(--blueprint-violet,#7c3aed)]" />,
  },
];

export function DemoPage({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const requested = new URLSearchParams(window.location.search).get("role");
  const [busy, setBusy] = useState<DemoPersona | null>(null);
  const [lastAttempt, setLastAttempt] = useState<DemoPersona | null>(null);
  const [error, setError] = useState("");
  const [elapsed, setElapsed] = useState(0);
  const abortControllerRef = useRef<AbortController | null>(null);
  const { play } = useAudioEngine();

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
    play("tab");
    setBusy(persona);
    setLastAttempt(persona);
    setError("");
    setElapsed(0);

    const controller = new AbortController();
    abortControllerRef.current = controller;

    try {
      const response = await authClient.demo(persona, controller.signal);
      if (controller.signal.aborted) return;
      play("success");
      onAuthenticated(response.token);
    } catch (failure) {
      play("error");
      if (controller.signal.aborted) {
        setError("Sign-in cancelled. The connection wait was aborted.");
      } else {
        setError(
          failure instanceof Error
            ? failure.message
            : "Demo access is unavailable. Please try again."
        );
      }
    } finally {
      if (abortControllerRef.current === controller) {
        setBusy(null);
        abortControllerRef.current = null;
      }
    }
  }

  function handleCancel() {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    setBusy(null);
  }

  const progressFraction = Math.min(1, elapsed / 60);

  return (
    <div className="min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors relative">
      <div className="pointer-events-none absolute inset-x-0 top-0 h-80 tx-aurora-horizon" aria-hidden="true" />
      <PublicHeader />

      <main className="relative z-10 flex-1 w-full max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-10 sm:py-14 space-y-10">
        {/* Page Heading */}
        <div className="space-y-3 max-w-2xl">
          <div className="inline-flex items-center gap-2 font-mono text-xs font-semibold text-[var(--blueprint-cobalt,#2563eb)]">
            <DotSentinel state={busy ? "probing" : error ? "error" : "idle"} />
            <ScrambleText text="EXPLORE THE LIVE SIMULATED NETWORK" />
          </div>
          <h1 className="font-display text-3xl sm:text-4xl lg:text-5xl font-bold tracking-tight text-[var(--text-primary)]">
            One system.<br />
            <span className="text-[var(--text-secondary)]">Choose your perspective.</span>
          </h1>
          <p className="demo-lead text-sm text-[var(--text-secondary)] leading-relaxed">
            No registration needed. These shared synthetic accounts use simulated money. Their activity is visible to other demo visitors.
          </p>
        </div>

        {/* Demo Notice Banner */}
        <div className="demo-notice p-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)]/90 backdrop-blur-sm text-xs flex items-start gap-3 shadow-[var(--shadow-subtle)]">
          <span className="text-[var(--action-accent-bg)] font-bold text-sm" aria-hidden="true">
            ↳
          </span>
          <p className="text-[var(--text-secondary)] leading-relaxed m-0">
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
            className="p-4 rounded-xl border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-xs text-[var(--status-danger-fg)] flex flex-col sm:flex-row sm:items-start justify-between gap-3 shadow-sm"
          >
            <div className="flex items-start gap-2.5">
              <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
              <div className="space-y-1">
                <p className="font-semibold m-0">{error}</p>
                <p className="opacity-90 m-0">
                  You can retry the connection, inspect <a href="/showcase" className="underline font-semibold">/showcase</a> offline scenarios, or sign in with private credentials.
                </p>
              </div>
            </div>
            {busy === null && (
              <div className="flex items-center gap-2 shrink-0">
                {lastAttempt && (
                  <button
                    type="button"
                    onClick={() => void enter(lastAttempt)}
                    className="inline-flex items-center gap-1 rounded-md border border-[var(--status-danger-border)] bg-[var(--bg-surface)]/80 px-2.5 py-1 text-[var(--status-danger-fg)] font-mono text-xs font-semibold hover:opacity-90 cursor-pointer"
                  >
                    <RefreshCw className="w-3 h-3" aria-hidden="true" />
                    Retry
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => setError("")}
                  className="text-[var(--status-danger-fg)] hover:opacity-75 cursor-pointer text-xs font-mono px-2 py-1"
                >
                  Dismiss
                </button>
              </div>
            )}
          </div>
        )}

        {/* 3 Primary Persona Cards */}
        <StaggerGroup className="role-cards grid grid-cols-1 md:grid-cols-3 gap-6">
          {personas.map((persona, index) => {
            const isRecommended = requested === persona.key;
            const isThisBusy = busy === persona.key;

            return (
              <StaggerItem key={persona.key}>
                <SpotlightCard
                  as="section"
                  accent={persona.accentColor}
                  data-cursor="Select Role"
                  className={`role-card demo-role h-full flex flex-col justify-between p-6 rounded-2xl border transition-all ${
                    isRecommended
                      ? "is-recommended border-[var(--action-accent-bg)] bg-[var(--bg-surface)] shadow-md ring-2 ring-[var(--action-accent-bg)]/20"
                      : "border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-[var(--shadow-subtle)] hover:border-[var(--border-active)]"
                  }`}
                >
                  <div className="relative z-10 space-y-4">
                    <div className="role-card-top flex items-center justify-between">
                      <span className="role-icon p-2.5 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]">
                        {persona.icon}
                      </span>
                      <span className="role-number font-mono text-xs font-bold text-[var(--text-secondary)]">
                        0{index + 1}
                      </span>
                    </div>

                    <div>
                      <span className="role-name text-[11px] font-mono text-[var(--text-secondary)] block">
                        {persona.name}
                      </span>
                      <h2 className="font-display text-xl font-bold tracking-tight text-[var(--text-primary)] mt-0.5">
                        {persona.title}
                      </h2>
                    </div>

                    <p className="text-xs text-[var(--text-secondary)] leading-relaxed">
                      {persona.description}
                    </p>

                    <div className="pt-1">
                      <span className="inline-block text-[10px] font-mono px-2 py-0.5 rounded bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)] text-[var(--text-secondary)]">
                        {persona.roleScope}
                      </span>
                    </div>
                  </div>

                  <div className="relative z-10 pt-6">
                    <Button
                      variant="primary"
                      className="w-full justify-center gap-2 group/btn"
                      disabled={busy !== null}
                      onClick={() => void enter(persona.key)}
                    >
                      <span>
                        {isThisBusy ? "Connecting…" : `Explore ${persona.title.toLowerCase()}`}
                      </span>
                      {!isThisBusy && (
                        <ArrowRight className="w-3.5 h-3.5 transition-transform group-hover/btn:translate-x-0.5" />
                      )}
                    </Button>
                  </div>
                </SpotlightCard>
              </StaggerItem>
            );
          })}
        </StaggerGroup>

        {/* Live Elapsed Progress Ring & Cancellation */}
        <div className="flex flex-col sm:flex-row items-center justify-between gap-4 p-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/40 text-xs">
          <div className="demo-progress flex items-center gap-3 text-xs font-mono text-[var(--text-secondary)]" role="status">
            {busy ? (
              <>
                <svg className="w-5 h-5 shrink-0 -rotate-90" viewBox="0 0 24 24" aria-hidden="true">
                  <circle cx="12" cy="12" r="9" fill="none" stroke="var(--border-hairline)" strokeWidth="2.5" />
                  <circle
                    cx="12"
                    cy="12"
                    r="9"
                    fill="none"
                    stroke="var(--action-accent-bg)"
                    strokeWidth="2.5"
                    strokeDasharray={56.5}
                    strokeDashoffset={56.5 * (1 - progressFraction)}
                    strokeLinecap="round"
                  />
                </svg>
                <span>
                  {elapsed < 5
                    ? "Waking sandbox…"
                    : `Checking backend health (${elapsed}s)`}{" "}
                  ·{" "}
                  <a
                    href="/showcase"
                    className="font-semibold text-[var(--action-accent-bg)] underline hover:opacity-80"
                  >
                    Browse read-only scenarios (/showcase)
                  </a>{" "}
                  while waiting.
                </span>
              </>
            ) : (
              <span className="flex items-center gap-2">
                <Clock className="w-3.5 h-3.5 text-[var(--text-tertiary)] shrink-0" aria-hidden="true" />
                <span>
                  Shared demo accounts are separate from private sign-in. Public operator demo is strictly read-only (OPS_VIEWER).
                </span>
              </span>
            )}
          </div>

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
        <SpotlightCard
          tilt={false}
          className="demo-secondary p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] flex flex-col sm:flex-row items-center justify-between gap-4"
        >
          <div className="relative z-10">
            <h3 className="text-sm font-semibold text-[var(--text-primary)]">
              Need the other side of a transfer?
            </h3>
            <p className="text-xs text-[var(--text-secondary)] mt-0.5">
              Launch Bob's receiving account at Bank B to watch transfers arrive.
            </p>
          </div>

          <div className="relative z-10 flex items-center gap-3">
            <Button
              variant="secondary"
              disabled={busy !== null}
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
        </SpotlightCard>
      </main>
    </div>
  );
}
