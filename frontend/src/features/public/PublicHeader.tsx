import React, { useEffect, useState } from "react";
import { Menu, X, ArrowRight, Command } from "lucide-react";
import { BrandMark } from "../../components";
import { ThemeToggle } from "../../components/theme/ThemeToggle";
import { SoundToggle, openCommandPalette, useAudioEngine } from "../../components/effects";

const sourceRepoUrl = "https://github.com/whoevenisshubham/TransactX";

export function PublicHeader() {
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const { play } = useAudioEngine();

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 18);
    onScroll();
    window.addEventListener("scroll", onScroll, { passive: true });
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  return (
    <header
      className={`sticky top-0 z-40 w-full border-b transition-all duration-200 ${
        scrolled
          ? "border-[var(--border-active)] bg-[var(--bg-surface)]/88 backdrop-blur-lg shadow-[var(--shadow-subtle)]"
          : "border-[var(--border-hairline)] bg-[var(--bg-surface)]/95 backdrop-blur-md"
      }`}
    >
      <div className="mx-auto flex h-16 max-w-7xl items-center justify-between gap-3 px-4 sm:px-6 lg:px-8">
        <a
          className="inline-flex items-center gap-2.5 text-base font-display font-bold tracking-tight text-[var(--text-primary)] no-underline shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] rounded-md px-1 py-0.5"
          href="/"
          aria-label="TransactX home"
        >
          <BrandMark />
          <span>
            TransactX<span className="text-[var(--blueprint-cobalt,#2563eb)]">.</span>
          </span>
        </a>

        {/* Desktop Navigation (>= 1024px) */}
        <nav aria-label="Public navigation" className="hidden lg:flex items-center gap-4 xl:gap-5">
          <a
            className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline whitespace-nowrap"
            href="/#how-it-works"
            onClick={() => play("tick")}
          >
            How it works
          </a>
          <a
            className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline whitespace-nowrap"
            href="/showcase"
            onClick={() => play("tick")}
          >
            Scenarios
          </a>
          <a
            className="text-xs font-medium text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline whitespace-nowrap"
            href="/privacy"
            onClick={() => play("tick")}
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

          <button
            type="button"
            onClick={() => {
              play("modal");
              openCommandPalette();
            }}
            aria-label="Open command palette"
            className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-2.5 py-1.5 text-[11px] font-mono text-[var(--text-secondary)] hover:border-[var(--border-active)] hover:text-[var(--text-primary)] transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] whitespace-nowrap"
          >
            <Command className="w-3 h-3" aria-hidden="true" />
            <span>Search</span>
            <kbd className="rounded bg-[var(--bg-surface)] px-1 py-0.5 text-[10px] border border-[var(--border-hairline)]">
              ⌘K
            </kbd>
          </button>

          <SoundToggle compact />

          <ThemeToggle compact />

          <div className="flex items-center gap-2">
            <a
              className="inline-flex items-center justify-center rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-3.5 py-2 text-xs font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] transition-colors no-underline whitespace-nowrap focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
              href="/login"
              onClick={() => play("tick")}
            >
              Sign in
            </a>
            <a
              className="group inline-flex items-center justify-center gap-1.5 rounded-lg bg-[var(--action-primary-bg)] px-3.5 py-2 text-xs font-semibold !text-[var(--action-primary-fg)] hover:opacity-90 active:scale-[0.98] transition-all no-underline whitespace-nowrap focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)]"
              href="/demo"
              onClick={() => play("tab")}
            >
              <span>Explore demo</span>
              <ArrowRight className="w-3.5 h-3.5 transition-transform group-hover:translate-x-0.5" aria-hidden="true" />
            </a>
          </div>
        </nav>

        {/* Tablet & Mobile Controls (< 1024px) */}
        <div className="flex lg:hidden items-center gap-1.5 sm:gap-2">
          <SoundToggle compact />
          <ThemeToggle compact />
          <a
            className="hidden sm:inline-flex items-center justify-center rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-2.5 py-1.5 text-xs font-semibold text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] transition-colors no-underline whitespace-nowrap"
            href="/login"
          >
            Sign in
          </a>
          <button
            type="button"
            onClick={() => {
              play("tick");
              setMobileMenuOpen((v) => !v);
            }}
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
        <div className="lg:hidden border-t border-[var(--border-hairline)] bg-[var(--bg-surface)] px-4 py-4 space-y-3 shadow-lg">
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
              Scenarios & Playground
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
              onClick={() => setMobileMenuOpen(false)}
            >
              Source code
            </a>
            <button
              type="button"
              onClick={() => {
                setMobileMenuOpen(false);
                openCommandPalette();
              }}
              className="flex items-center justify-between rounded-lg px-3 py-2 text-sm font-medium text-[var(--text-secondary)] hover:bg-[var(--bg-surface-raised)] hover:text-[var(--text-primary)] text-left cursor-pointer"
            >
              <span>Command palette</span>
              <kbd className="font-mono text-xs opacity-70">⌘K</kbd>
            </button>
          </nav>
          <div className="flex items-center gap-2 pt-2 border-t border-[var(--border-hairline)]">
            <a
              className="flex-1 inline-flex items-center justify-center rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] px-3 py-2.5 text-xs font-semibold text-[var(--text-primary)] no-underline"
              href="/login"
              onClick={() => setMobileMenuOpen(false)}
            >
              Sign in
            </a>
            <a
              className="flex-1 inline-flex items-center justify-center gap-1.5 rounded-lg bg-[var(--action-primary-bg)] px-3 py-2.5 text-xs font-semibold !text-[var(--action-primary-fg)] no-underline"
              href="/demo"
              onClick={() => setMobileMenuOpen(false)}
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
