import React from "react";
import { BrandMark } from "../../components";

const source = "https://github.com/whoevenisshubham/TransactX";

export function PublicFooter() {
  return (
    <footer className="public-footer public-container">
      <a href="/" className="brand-lockup">
        <BrandMark />
        TransactX
      </a>
      <p>A simulated network. No real money moves.</p>
      <nav aria-label="Footer">
        <a href={source} target="_blank" rel="noreferrer">
          GitHub
        </a>
        <a
          href={`${source}/blob/release/transactx-v2/docs/architecture.md`}
          target="_blank"
          rel="noreferrer"
        >
          Architecture
        </a>
        <a href="/showcase">Scenarios</a>
        <a href="/privacy">Privacy</a>
      </nav>
    </footer>
  );
}
