import React from "react";
import { ArrowRight, ArrowDown } from "lucide-react";
import { PublicHeader } from "./PublicHeader";
import { PaymentScene } from "./PaymentScene";
import { WorkspaceExplorer } from "./WorkspaceExplorer";
import { TwoPhaseNarrative } from "./TwoPhaseNarrative";
import { ArchitecturalPillars } from "./ArchitecturalPillars";
import { PublicFooter } from "./PublicFooter";

export function LandingPage() {
  return (
    <div className="min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <PublicHeader />

      <main id="main" className="flex-1">
        {/* Hero */}
        <section className="mx-auto max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:px-8 lg:py-20">
          <div className="grid grid-cols-1 items-center gap-12 lg:grid-cols-12 lg:gap-10">
            <div className="lg:col-span-6 space-y-6">
              <div className="inline-flex items-center gap-2 rounded-full border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3 py-1 text-xs font-mono font-semibold text-[var(--accent-strong)]">
                <span className="h-1.5 w-1.5 rounded-full bg-[var(--action-accent-bg)]" aria-hidden="true" />
                <span>Payments, with perspective</span>
              </div>

              <h1 className="text-4xl font-extrabold tracking-tight text-[var(--text-primary)] sm:text-5xl lg:text-[54px] lg:leading-[1.06]">
                Money moves.
                <br />
                <span className="text-[var(--text-secondary)]">The facts stay.</span>
              </h1>

              <p className="max-w-xl text-base leading-relaxed text-[var(--text-secondary)] sm:text-lg">
                Send it. Receive it. Understand it. A simulated payment network that makes the whole
                journey visible—even when a reply goes missing.
              </p>

              <div className="flex flex-wrap items-center gap-4 pt-1">
                <a
                  className="inline-flex items-center justify-center gap-2 rounded-lg bg-[var(--action-primary-bg)] px-5 py-3 text-sm font-semibold !text-[var(--action-primary-fg)] hover:opacity-90 transition-opacity no-underline shadow-[var(--shadow-subtle)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
                  href="/demo"
                >
                  <span>Explore the demo</span>
                  <ArrowRight className="w-4 h-4" aria-hidden="true" />
                </a>
                <a
                  className="inline-flex items-center gap-1.5 rounded-lg px-3 py-2.5 text-sm font-semibold text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
                  href="#workspaces"
                >
                  <span>Find your workspace</span>
                  <ArrowDown className="w-4 h-4" aria-hidden="true" />
                </a>
              </div>

              <div className="flex items-center gap-2 pt-2 text-xs font-mono text-[var(--text-tertiary)]">
                <span className="h-2 w-2 rounded-full bg-[var(--status-success-fg)]" aria-hidden="true" />
                <span>Simulated money. Actual system behaviour. Integer paise precision.</span>
              </div>
            </div>

            <div className="lg:col-span-6">
              <PaymentScene />
            </div>
          </div>
        </section>

        <WorkspaceExplorer />

        <TwoPhaseNarrative />

        <ArchitecturalPillars />

        {/* Closing CTA */}
        <section className="mx-auto max-w-7xl px-4 py-14 sm:px-6 lg:px-8 lg:py-20">
          <div className="flex flex-col items-start justify-between gap-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-8 shadow-[var(--shadow-subtle)] sm:flex-row sm:items-center sm:p-10">
            <div className="space-y-2">
              <p className="text-xs font-mono font-semibold text-[var(--action-accent-bg)] m-0">
                Open the system. Follow a payment.
              </p>
              <h2 className="text-2xl font-bold tracking-tight text-[var(--text-primary)] sm:text-3xl m-0">
                See it from every side.
              </h2>
            </div>
            <a
              className="inline-flex items-center justify-center gap-2 rounded-lg bg-[var(--action-primary-bg)] px-5 py-3 text-sm font-semibold !text-[var(--action-primary-fg)] hover:opacity-90 transition-opacity no-underline shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
              href="/demo"
            >
              <span>Choose a demo role</span>
              <ArrowRight className="w-4 h-4" aria-hidden="true" />
            </a>
          </div>
        </section>
      </main>

      <PublicFooter />
    </div>
  );
}
