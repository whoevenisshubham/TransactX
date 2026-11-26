import React, { useState } from "react";
import { Menu, X, ArrowRight } from "lucide-react";
import { BrandMark } from "../../components";
import { ThemeToggle } from "../../components/theme/ThemeToggle";

const sourceRepoUrl = "https://github.com/whoevenisshubham/TransactX";

export function PublicHeader() {
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  return (
    <header className="sticky top-0 z-40 w-full border-b border-[var(--border-hairline)] bg-[var(--bg-surface)]/95 backdrop-blur-md">
      <div className="mx-auto flex h-16 max-w-7xl items-center justify-between gap-4 px-4 sm:px-6 lg:px-8">
        <a
          className="inline-flex items-center gap-2.5 text-base font-bold tracking-tight text-[var(--text-primary)] no-underline shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] rounded-md px-1 py-0.5"
          href="/"
          aria-label="TransactX home"
        >
          <BrandMark />
          <span>
            TransactX<span className="text-[var(--action-accent-bg)]">.</span>
          </span>
        </a>

        {/* Desktop Navigation (>= 1024px) */}
        <nav aria-label="Public navigation" className="hidden lg:flex items-center gap-5 lg:gap-6">
          <a
            className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline whitespace-nowrap"
            href="/#how-it-works"
          >
            How it works
          </a>
          <a
            className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline whitespace-nowrap"
            href="/showcase"
          >
            Scenarios
          </a>
          <a
            className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline whitespace-nowrap"
            href="/privacy"
          >
            Privacy
          </a>
          <a
            className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline whitespace-nowrap"
            href={sourceRepoUrl}
            target="_blank"
            rel="noreferrer"
          >
            Source code
          </a>

          <div className="h-4 w-px bg-[var(--border-hairline)]" aria-hidden="true" />

          <ThemeToggle compact />

          <div className="flex items-center gap-2.5">
            <a
              className="inline-flex items-center justify-center rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3.5 py-2 text-xs font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] transition-colors no-underline whitespace-nowrap focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
              href="/login"
            >
              Sign in
            </a>
            <a
              className="inline-flex items-center justify-center gap-1.5 rounded-lg bg-[var(--action-primary-bg)] px-3.5 py-2 text-xs font-semibold !text-[var(--action-primary-fg)] hover:opacity-90 transition-opacity no-underline whitespace-nowrap focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
              href="/demo"
            >
              <span>Explore demo</span>
              <ArrowRight className="w-3.5 h-3.5" aria-hidden="true" />
            </a>
          </div>
        </nav>

        {/* Tablet & Mobile Controls (< 1024px) */}
        <div className="flex lg:hidden items-center gap-2">
          <ThemeToggle compact />
          <a
            className="hidden sm:inline-flex items-center justify-center rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-xs font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] transition-colors no-underline whitespace-nowrap"
            href="/login"
          >
            Sign in
          </a>
          <button
            type="button"
            onClick={() => setMobileMenuOpen((v) => !v)}
            aria-expanded={mobileMenuOpen}
            aria-label={mobileMenuOpen ? "Close navigation menu" : "Open navigation menu"}
            className="inline-flex items-center justify-center w-9 h-9 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] text-[var(--text-primary)] cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
          >
            {mobileMenuOpen ? <X className="w-4 h-4" aria-hidden="true" /> : <Menu className="w-4 h-4" aria-hidden="true" />}
          </button>
        </div>
      </div>

      {/* Tablet & Mobile Dropdown Drawer */}
      {mobileMenuOpen && (
        <div className="lg:hidden border-t border-[var(--border-hairline)] bg-[var(--bg-surface)] px-4 py-4 space-y-3">
          <nav aria-label="Mobile public navigation" className="flex flex-col gap-1">
            <a
              className="rounded-lg px-3 py-2 text-sm font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-surface-raised)] hover:text-[var(--text-primary)] no-underline"
              href="/#how-it-works"
              onClick={() => setMobileMenuOpen(false)}
            >
              How it works
            </a>
            <a
              className="rounded-lg px-3 py-2 text-sm font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-surface-raised)] hover:text-[var(--text-primary)] no-underline"
              href="/showcase"
              onClick={() => setMobileMenuOpen(false)}
            >
              Scenarios
            </a>
            <a
              className="rounded-lg px-3 py-2 text-sm font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-surface-raised)] hover:text-[var(--text-primary)] no-underline"
              href="/privacy"
              onClick={() => setMobileMenuOpen(false)}
            >
              Privacy
            </a>
            <a
              className="rounded-lg px-3 py-2 text-sm font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-surface-raised)] hover:text-[var(--text-primary)] no-underline"
              href={sourceRepoUrl}
              target="_blank"
              rel="noreferrer"
            >
              Source code
            </a>
          </nav>
          <div className="pt-2 border-t border-[var(--border-hairline)] flex flex-col gap-2">
            <a
              className="inline-flex w-full items-center justify-center rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-4 py-2 text-xs font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] transition-colors no-underline"
              href="/login"
            >
              Sign in
            </a>
            <a
              className="inline-flex w-full items-center justify-center gap-1.5 rounded-lg bg-[var(--action-primary-bg)] px-4 py-2.5 text-xs font-semibold !text-[var(--action-primary-fg)] hover:opacity-90 transition-opacity no-underline"
              href="/demo"
            >
              <span>Explore demo</span>
              <ArrowRight className="w-3.5 h-3.5" aria-hidden="true" />
            </a>
          </div>
        </div>
      )}
    </header>
  );
}
