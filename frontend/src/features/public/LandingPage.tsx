import React from "react";
import { motion, useReducedMotion } from "motion/react";
import { ArrowRight, ArrowDown, ShieldCheck, Cpu, Database, Lock, GitCommitHorizontal } from "lucide-react";
import { PublicHeader } from "./PublicHeader";
import { PaymentScene } from "./PaymentScene";
import { WorkspaceExplorer } from "./WorkspaceExplorer";
import { TwoPhaseNarrative } from "./TwoPhaseNarrative";
import { ArchitecturalPillars } from "./ArchitecturalPillars";
import { PublicFooter } from "./PublicFooter";
import {
  ScrollProgressBar,
  Reveal,
  AnimatedCounter,
  MagneticButton,
  ScrambleText,
  SpotlightCard,
  useAudioEngine,
} from "../../components/effects";

const PROTOCOL_MARQUEE = [
  "Integer Paise Arithmetic (BigInt / int64)",
  "Fenced HeartbeatLease Recovery",
  "SHA-256 merkle-v1 Bucket Trees",
  "Isolated bank_a & bank_b Schemas",
  "UUIDv4 Client Outbox Reservation",
  "Adaptive CLOSED / HALF_OPEN / OPEN Breakers",
  "Strictly Read-Only OPS_VIEWER RBAC",
];

const EVIDENCE_BENCHMARKS = [
  { label: "Unit & domain invariant checks", value: 134, suffix: "", note: "Money, outbox & state machines" },
  { label: "Browser + Axe automated specs", value: 157, suffix: "+", note: "WCAG AA verified 320px–1440px" },
  { label: "Deterministic scenario records", value: 192, suffix: "", note: "8 guided stories in /showcase" },
  { label: "Floating-point rupee drift", value: 0, suffix: "", note: "Strict integer paise math end-to-end" },
];

export function LandingPage() {
  const reduce = useReducedMotion();
  const { play } = useAudioEngine();

  return (
    <div className="min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors relative">
      <ScrollProgressBar />
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <PublicHeader />

      <main id="main" className="flex-1">
        {/* Hero with Aurora Horizon & Structural Guide Lines */}
        <section className="relative overflow-hidden border-b border-[var(--border-hairline)]">
          <div className="pointer-events-none absolute inset-0 tx-aurora-horizon" aria-hidden="true" />
          <div
            className="pointer-events-none mx-auto max-w-7xl border-x border-[var(--border-hairline)]/40 absolute inset-0"
            aria-hidden="true"
          />

          <div className="relative z-10 mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8 lg:py-20">
            <div className="grid grid-cols-1 items-center gap-12 lg:grid-cols-12 lg:gap-10">
              <motion.div
                initial={reduce ? undefined : { opacity: 0.98, y: 14 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.42, ease: [0.22, 1, 0.36, 1] }}
                className="lg:col-span-6 space-y-6"
              >
                <div className="inline-flex items-center gap-2 rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface)]/90 px-3 py-1 text-xs font-mono font-semibold text-[var(--blueprint-cobalt,#2563eb)] shadow-[var(--shadow-subtle)]">
                  <span className="h-1.5 w-1.5 rounded-full bg-[var(--blueprint-emerald,#10b981)] animate-pulse" aria-hidden="true" />
                  <ScrambleText text="Payments, with perspective" />
                </div>

                <h1 className="font-display text-4xl font-bold tracking-tight text-[var(--text-primary)] sm:text-5xl lg:text-[56px] lg:leading-[1.04]">
                  Money moves.
                  <br />
                  <span className="bg-gradient-to-r from-[var(--text-secondary)] via-[var(--blueprint-cobalt,#2563eb)] to-[var(--blueprint-emerald,#10b981)] bg-clip-text text-transparent">
                    The facts stay.
                  </span>
                </h1>

                <p className="max-w-xl text-base leading-relaxed text-[var(--text-secondary)] sm:text-lg">
                  Send it. Receive it. Understand it. A simulated fault-tolerant payment switch that
                  keeps every bank observation, saga transition, and cryptographic proof intact—even
                  when a wire reply goes missing.
                </p>

                <div className="flex flex-wrap items-center gap-4 pt-1">
                  <MagneticButton href="/demo" variant="primary">
                    <span>Explore the demo</span>
                    <ArrowRight className="w-4 h-4 transition-transform group-hover/mag:translate-x-0.5" aria-hidden="true" />
                  </MagneticButton>
                  <MagneticButton href="#workspaces" variant="secondary">
                    <span>Find your workspace</span>
                    <ArrowDown className="w-4 h-4" aria-hidden="true" />
                  </MagneticButton>
                </div>

                <div className="flex flex-wrap items-center gap-x-4 gap-y-2 pt-2 text-xs font-mono text-[var(--text-tertiary)]">
                  <span className="inline-flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-full bg-[var(--status-success-fg)]" aria-hidden="true" />
                    Simulated money. Actual system behaviour. Integer paise precision.
                  </span>
                </div>

                {/* Stack badges */}
                <div className="flex flex-wrap items-center gap-2 pt-1">
                  {[
                    { icon: <Cpu className="w-3 h-3" />, text: "Go Saga Switch" },
                    { icon: <Database className="w-3 h-3" />, text: "PostgreSQL Outbox" },
                    { icon: <ShieldCheck className="w-3 h-3" />, text: "merkle-v1 Proofs" },
                    { icon: <Lock className="w-3 h-3" />, text: "RBAC OPS_VIEWER" },
                  ].map((item) => (
                    <span
                      key={item.text}
                      className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border-hairline)] bg-[var(--bg-surface)]/80 px-2.5 py-1 text-[11px] font-mono text-[var(--text-secondary)]"
                    >
                      {item.icon}
                      <span>{item.text}</span>
                    </span>
                  ))}
                </div>
              </motion.div>

              <motion.div
                initial={reduce ? undefined : { opacity: 0.98, y: 16 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.44, delay: 0.1, ease: [0.22, 1, 0.36, 1] }}
                className="lg:col-span-6"
              >
                <PaymentScene />
              </motion.div>
            </div>
          </div>

          {/* Continuous Protocol Marquee Ribbon */}
          <div className="border-t border-[var(--border-hairline)] bg-[var(--bg-surface)]/75 py-2.5 overflow-hidden">
            <div className="tx-marquee-track items-center gap-8 text-[11px] font-mono text-[var(--text-tertiary)]">
              {[...PROTOCOL_MARQUEE, ...PROTOCOL_MARQUEE].map((item, idx) => (
                <span key={`${item}-${idx}`} className="inline-flex items-center gap-2 whitespace-nowrap px-2">
                  <GitCommitHorizontal className="w-3.5 h-3.5 text-[var(--blueprint-cobalt,#2563eb)]" aria-hidden="true" />
                  <span>{item}</span>
                </span>
              ))}
            </div>
          </div>
        </section>

        {/* Scroll Counter Benchmarks Strip */}
        <section className="border-b border-[var(--border-hairline)] bg-[var(--bg-surface)]">
          <div className="mx-auto max-w-7xl px-4 py-10 sm:px-6 lg:px-8">
            <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
              {EVIDENCE_BENCHMARKS.map((stat) => (
                <SpotlightCard
                  key={stat.label}
                  tilt={false}
                  className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-canvas)]/70 p-4"
                >
                  <div className="relative z-10 space-y-1">
                    <div className="font-display text-2xl sm:text-3xl font-bold text-[var(--text-primary)]">
                      <AnimatedCounter value={stat.value} suffix={stat.suffix} />
                    </div>
                    <div className="text-xs font-semibold text-[var(--text-primary)]">{stat.label}</div>
                    <div className="text-[11px] font-mono text-[var(--text-tertiary)]">{stat.note}</div>
                  </div>
                </SpotlightCard>
              ))}
            </div>
          </div>
        </section>

        <Reveal>
          <WorkspaceExplorer />
        </Reveal>

        <Reveal>
          <TwoPhaseNarrative />
        </Reveal>

        <Reveal>
          <ArchitecturalPillars />
        </Reveal>

        {/* Closing CTA */}
        <section className="mx-auto max-w-7xl px-4 py-14 sm:px-6 lg:px-8 lg:py-20">
          <SpotlightCard
            accent="rgba(37, 99, 235, 0.16)"
            className="flex flex-col items-start justify-between gap-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-8 shadow-[var(--shadow-raised)] sm:flex-row sm:items-center sm:p-10"
          >
            <div className="relative z-10 space-y-2">
              <p className="text-xs font-mono font-semibold text-[var(--action-accent-bg)] m-0">
                Open the system. Follow a payment.
              </p>
              <h2 className="font-display text-2xl font-bold tracking-tight text-[var(--text-primary)] sm:text-3xl m-0">
                See it from every side.
              </h2>
            </div>
            <div className="relative z-10 flex flex-wrap items-center gap-3 shrink-0">
              <MagneticButton href="/showcase" variant="secondary" onClick={() => play("tick")}>
                <span>Open interactive playground</span>
              </MagneticButton>
              <MagneticButton href="/demo" variant="primary" onClick={() => play("tab")}>
                <span>Choose a demo role</span>
                <ArrowRight className="w-4 h-4 transition-transform group-hover/mag:translate-x-0.5" aria-hidden="true" />
              </MagneticButton>
            </div>
          </SpotlightCard>
        </section>
      </main>

      <PublicFooter />
    </div>
  );
}
