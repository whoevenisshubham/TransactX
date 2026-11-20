import React from "react";
import { PublicHeader } from "./PublicHeader";
import { PublicFooter } from "./PublicFooter";
import { ShieldCheck, Database, HardDrive, Cpu, KeyRound, MessageSquare } from "lucide-react";

export function PrivacyPage() {
  return (
    <div className="public-site reset-public reset-access min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors">
      <PublicHeader />

      <main className="public-container privacy-page flex-1 w-full max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-10 sm:py-16 space-y-12">
        <header className="space-y-3">
          <p className="eyebrow text-xs font-mono font-semibold uppercase tracking-wider text-[var(--action-accent-bg)]">
            TransactX demo
          </p>
          <h1 className="text-3xl sm:text-4xl font-extrabold tracking-tight text-[var(--text-primary)]">
            Privacy & demo data
          </h1>
          <p className="page-description text-base text-[var(--text-secondary)] leading-relaxed max-w-2xl">
            A simulated network for exploring payment reliability. No real money moves.
          </p>
        </header>

        <div className="grid grid-cols-1 gap-8">
          {/* Shared demo accounts */}
          <section className="p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm space-y-3">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-[var(--action-accent-bg)]/10 text-[var(--action-accent-bg)] flex items-center justify-center font-bold text-sm">
                <ShieldCheck className="w-4 h-4" />
              </div>
              <h2 className="text-xl font-bold tracking-tight text-[var(--text-primary)]">
                Shared demo accounts
              </h2>
            </div>
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
              Demo identities and their payment activity are shared with other visitors. Payment notes and investigation questions may appear in the operator workspace. Use synthetic information only; never enter bank details, personal information or secrets.
            </p>
          </section>

          {/* Data on this device */}
          <section className="p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm space-y-3">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-[var(--action-accent-bg)]/10 text-[var(--action-accent-bg)] flex items-center justify-center font-bold text-sm">
                <HardDrive className="w-4 h-4" />
              </div>
              <h2 className="text-xl font-bold tracking-tight text-[var(--text-primary)]">
                Data on this device
              </h2>
            </div>
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
              The application keeps its sign-in token and a minimal profile in session storage. Offline payment intents are kept in IndexedDB and separated by account. Logging out removes the session but retains saved intents for their original account. Your appearance preference is stored locally and survives logout.
            </p>
          </section>

          {/* Network and evidence */}
          <section className="p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm space-y-3">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-[var(--action-accent-bg)]/10 text-[var(--action-accent-bg)] flex items-center justify-center font-bold text-sm">
                <Database className="w-4 h-4" />
              </div>
              <h2 className="text-xl font-bold tracking-tight text-[var(--text-primary)]">
                Network and evidence
              </h2>
            </div>
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
              Payment records, bank observations and saved investigations are stored by the demo backend. Reliability evidence is retained rather than rewritten to change an outcome. Illustrative examples are labelled and are separate from live observations.
            </p>
          </section>

          {/* External services */}
          <section className="p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm space-y-3">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-[var(--action-accent-bg)]/10 text-[var(--action-accent-bg)] flex items-center justify-center font-bold text-sm">
                <Cpu className="w-4 h-4" />
              </div>
              <h2 className="text-xl font-bold tracking-tight text-[var(--text-primary)]">
                External services
              </h2>
            </div>
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
              The frontend is hosted on Vercel, the demo services on Render and the database on Neon. These services process network requests under their own terms. Fonts are served with the application. No advertising or analytics tracker is embedded in this application.
            </p>
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
              Google sign-in and model-assisted investigations, when enabled, require separate provider configuration. An evidence-only report does not imply a model call. Never include personal information in a report question.
            </p>
          </section>

          {/* Google sign-in */}
          <section className="p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm space-y-3">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-[var(--action-accent-bg)]/10 text-[var(--action-accent-bg)] flex items-center justify-center font-bold text-sm">
                <KeyRound className="w-4 h-4" />
              </div>
              <h2 className="text-xl font-bold tracking-tight text-[var(--text-primary)]">
                Google sign-in
              </h2>
            </div>
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
              When enabled, Google handles consent and authentication. TransactX stores the verified issuer and subject with a generated public alias, and creates a zero-balance customer account. It does not store your Google name, email, access token or refresh token. Short-lived sign-in state and one-time codes expire; they are not payment evidence. Google identities are never linked to an existing account by email.
            </p>
          </section>

          {/* Contact and source */}
          <section className="p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm space-y-3">
            <div className="flex items-center gap-3">
              <div className="w-8 h-8 rounded-lg bg-[var(--action-accent-bg)]/10 text-[var(--action-accent-bg)] flex items-center justify-center font-bold text-sm">
                <MessageSquare className="w-4 h-4" />
              </div>
              <h2 className="text-xl font-bold tracking-tight text-[var(--text-primary)]">
                Contact and source
              </h2>
            </div>
            <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
              For a privacy question, contact the repository owner through{" "}
              <a
                href="https://github.com/whoevenisshubham/TransactX"
                target="_blank"
                rel="noreferrer"
                className="text-[var(--action-accent-bg)] underline underline-offset-2 font-medium hover:opacity-80"
              >
                the TransactX GitHub repository
              </a>
              . Do not post passwords, tokens or private account information in an issue.
            </p>
          </section>
        </div>

        <div className="pt-4">
          <a
            href="/demo"
            className="button button-primary inline-flex items-center justify-center px-5 py-2.5 rounded-lg bg-[var(--action-primary-bg)] text-[var(--action-primary-fg)] font-semibold text-sm hover:opacity-90 transition-opacity shadow-sm"
          >
            Explore shared demos
          </a>
        </div>
      </main>

      <PublicFooter />
    </div>
  );
}
