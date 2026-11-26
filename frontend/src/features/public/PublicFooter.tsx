import React from "react";
import { BrandMark } from "../../components";

const source = "https://github.com/whoevenisshubham/TransactX";

export function PublicFooter() {
  return (
    <footer className="border-t border-[var(--border-hairline)] bg-[var(--bg-surface)]">
      <div className="mx-auto flex max-w-7xl flex-col gap-6 px-4 py-10 sm:px-6 lg:flex-row lg:items-center lg:justify-between lg:px-8">
        <div className="flex flex-col gap-1.5 sm:flex-row sm:items-center sm:gap-4">
          <a
            href="/"
            className="inline-flex items-center gap-2 text-sm font-bold tracking-tight text-[var(--text-primary)] no-underline"
          >
            <BrandMark />
            <span>TransactX</span>
          </a>
          <p className="text-xs text-[var(--text-tertiary)] m-0">
            A simulated network. No real money moves.
          </p>
        </div>

        <nav aria-label="Footer" className="flex flex-wrap items-center gap-x-6 gap-y-2 text-xs font-medium">
          <a
            href={source}
            target="_blank"
            rel="noreferrer"
            className="text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline"
          >
            GitHub
          </a>
          <a
            href={`${source}/blob/release/transactx-v2/docs/architecture.md`}
            target="_blank"
            rel="noreferrer"
            className="text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline"
          >
            Architecture
          </a>
          <a
            href="/showcase"
            className="text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline"
          >
            Scenarios
          </a>
          <a
            href="/privacy"
            className="text-[var(--text-secondary)] hover:text-[var(--text-primary)] transition-colors no-underline"
          >
            Privacy
          </a>
        </nav>
      </div>
    </footer>
  );
}
