import React from "react";
import { PublicHeader } from "./PublicHeader";
import { PaymentScene } from "./PaymentScene";
import { WorkspaceExplorer } from "./WorkspaceExplorer";
import { TwoPhaseNarrative } from "./TwoPhaseNarrative";
import { PublicFooter } from "./PublicFooter";
import { Icon } from "../../components";

export function LandingPage() {
  return (
    <div className="public-site reset-public">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <PublicHeader />
      <main id="main">
        <section className="reset-hero public-container">
          <div className="reset-hero-copy">
            <p className="eyebrow">
              <span className="reset-brand-line" /> PAYMENTS, WITH PERSPECTIVE
            </p>
            <h1>
              Money moves.<br />
              <span>The facts stay.</span>
            </h1>
            <p className="reset-hero-description">
              Send it. Receive it. Understand it.
              <br />
              A simulated payment network that makes the whole journey visible—even when a reply goes missing.
            </p>
            <div className="hero-actions">
              <a className="button button-primary" href="/demo">
                Explore the demo <Icon name="arrow" />
              </a>
              <a className="reset-text-link" href="#workspaces">
                Find your workspace ↓
              </a>
            </div>
            <p className="reset-disclosure">
              <span aria-hidden="true" />
              Simulated money. Actual system behaviour.
            </p>
          </div>
          <PaymentScene />
        </section>

        <WorkspaceExplorer />

        <TwoPhaseNarrative />

        <section className="reset-closing public-container">
          <div>
            <p className="eyebrow">OPEN THE SYSTEM. FOLLOW A PAYMENT.</p>
            <h2>See it from every side.</h2>
          </div>
          <a className="button button-primary" href="/demo">
            Choose a demo role <Icon name="arrow" />
          </a>
        </section>
      </main>
      <PublicFooter />
    </div>
  );
}
