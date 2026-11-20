import React from "react";
import { BrandMark, Icon } from "../../components";
import { ThemeSelect } from "../../theme";

const sourceRepoUrl = "https://github.com/whoevenisshubham/TransactX";

export function PublicHeader() {
  return (
    <header className="public-header">
      <a className="brand-lockup" href="/" aria-label="TransactX home">
        <BrandMark />
        <span>
          TransactX<span className="brand-period">.</span>
        </span>
      </a>
      <nav aria-label="Public navigation">
        <a className="public-nav-link" href="/#how-it-works">
          How it works
        </a>
        <a className="public-nav-link" href={sourceRepoUrl} target="_blank" rel="noreferrer">
          Source code
        </a>
        <ThemeSelect />
        <a className="button button-secondary" href="/login">
          Sign in <Icon name="arrow" />
        </a>
      </nav>
    </header>
  );
}
