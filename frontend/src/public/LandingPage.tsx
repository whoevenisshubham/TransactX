import { BrandMark, Icon } from "../components";
import { ThemeSelect } from "../theme";

export function PublicHeader() {
  return <header className="public-header"><a className="brand-lockup" href="/" aria-label="TransactX home"><BrandMark /><span>TransactX<span className="brand-period">.</span></span></a><nav aria-label="Public navigation"><a className="public-nav-link" href="/#how-it-works">How it works</a><a className="public-nav-link" href="https://github.com/whoevenisshubham/TransactX">Source code</a><ThemeSelect /><a className="button button-secondary" href="/login">Sign in <Icon name="arrow" /></a></nav></header>;
}

function PaymentIllustration() {
  return <div className="payment-illustration">
    <div className="illustration-topline"><span className="illustration-label">Inside a payment</span><span className="example-label">Illustrative example</span></div>
    <div className="illustration-amount"><span>Customer → Merchant</span><strong>₹1,250<span>.00</span></strong><span className="illustration-reference">TX · EXAMPLE 0042</span></div>
    <svg className="payment-map" viewBox="0 0 520 238" role="img" aria-labelledby="payment-map-title payment-map-description">
      <title id="payment-map-title">A payment path with durable evidence</title><desc id="payment-map-description">Sender at Bank A, through the durable coordinator, to Bank B and the recipient. Bank observations and central state are separate evidence sources.</desc>
      <defs><pattern id="map-dots" width="18" height="18" patternUnits="userSpaceOnUse"><circle cx="1" cy="1" r="1" fill="currentColor" opacity=".12" /></pattern></defs>
      <rect width="520" height="238" fill="url(#map-dots)" />
      <g fill="none" stroke="currentColor" strokeWidth="1.5"><path className="map-line" d="M84 102H208M312 102H436" /><path className="map-branch" d="M260 134V181M84 132V155H436V132" strokeDasharray="4 5" /></g>
      <g className="map-node"><rect x="48" y="72" width="72" height="60" rx="12" /><rect x="400" y="72" width="72" height="60" rx="12" /></g>
      <g fill="none" stroke="currentColor" strokeWidth="1.6"><path d="m68 96 16-9 16 9H68Zm4 5v14m12-14v14m12-14v14m-28 5h32m-20-24h8M420 96l16-9 16 9h-32Zm4 5v14m12-14v14m12-14v14m-28 5h32" /></g>
      <rect className="map-hub" x="208" y="60" width="104" height="84" rx="16" />
      <g className="map-hub-icon" fill="none" stroke="currentColor" strokeWidth="2"><path d="m244 97 10 10 22-24m-32 31h32" /></g>
      <g className="map-label" textAnchor="middle"><text x="84" y="52">BANK A</text><text x="436" y="52">BANK B</text><text x="260" y="167">Durable coordinator</text><text x="84" y="189">Sender</text><text x="436" y="189">Recipient</text></g>
      <rect className="map-evidence" x="188" y="187" width="144" height="30" rx="6" /><text className="map-label" x="260" y="207" textAnchor="middle">Recorded evidence</text>
    </svg>
    <div className="illustration-evidence"><div><span className="evidence-index">01</span><span>Bank observations<strong>What participants reported</strong></span><span className="evidence-check">↗</span></div><div><span className="evidence-index">02</span><span>Central payment state<strong>The authoritative outcome</strong></span><span className="evidence-check">↗</span></div></div>
    <p className="illustration-note">A system diagram, not a live transaction.</p>
  </div>;
}

const roles = [
  { number: "01", role: "Customer", title: "Send with confidence.", copy: "Review a recipient, make a simulated payment, and follow its outcome.", detail: "Balance · Payments · Receipts", icon: "send" as const, persona: "customer" },
  { number: "02", role: "Merchant", title: "Make receiving simple.", copy: "Share a payment QR and reconcile incoming payments against their real status.", detail: "QR receive · Incoming · Settlement", icon: "qr" as const, persona: "merchant" },
  { number: "03", role: "Operator", title: "Follow the evidence.", copy: "Trace a payment across banks, inspect recovery, and ask grounded questions.", detail: "Journeys · Investigation · Analytics", icon: "activity" as const, persona: "operator" },
];

export function LandingPage() {
  return <div className="public-site"><a className="skip-link" href="#main">Skip to content</a><PublicHeader />
    <main id="main">
      <section className="landing-hero public-container"><div className="hero-copy"><p className="eyebrow"><span className="eyebrow-rule" />Distributed payment reliability</p><h1>Every payment.<br />Every failure.<br /><span>A clear explanation.</span></h1><p className="hero-description">Explore a payment system built to recover from interruptions, verify its ledgers, and explain what happened.</p><div className="hero-actions"><a className="button button-primary" href="/demo">Explore the demo <Icon name="arrow" /></a><a className="hero-secondary" href="#how-it-works">See how it works <span aria-hidden="true">↓</span></a></div><p className="hero-disclosure"><span className="disclosure-dot" />Simulated money. Real system behaviour.</p></div><PaymentIllustration /></section>
      <section className="role-section public-container" aria-labelledby="role-heading"><div className="landing-section-heading"><div><p className="eyebrow">Three perspectives. One system.</p><h2 id="role-heading">Choose your point of view.</h2></div><p>Explore the same network from both sides<br className="desktop-break" /> of a payment, and behind the scenes.</p></div><div className="role-cards">{roles.map(role => <a className="role-card" href={`/demo?role=${role.persona}`} key={role.role}><div className="role-card-top"><span className="role-icon"><Icon name={role.icon} size={22} /></span><span className="role-number">{role.number}</span></div><p className="role-name">{role.role}</p><h3>{role.title}</h3><p>{role.copy}</p><div className="role-card-foot"><span>{role.detail}</span><Icon name="arrow" /></div></a>)}</div></section>
      <section id="how-it-works" className="architecture-section"><div className="public-container"><div className="landing-section-heading"><div><p className="eyebrow">Built for the in-between</p><h2>When the response is missing,<br />the story shouldn’t be.</h2></div><p>A timeout is an observation. It is never<br className="desktop-break" /> enough to declare that money failed to move.</p></div><div className="capability-grid">{[
        ["01", "Recover durably", "Stable operation identities and a background worker keep recovery tied to the original payment."],
        ["02", "Observe both banks", "Compare central records with participant observations, including what is still unknown."],
        ["03", "Verify the ledger", "Inspect reconciliation and integrity evidence without changing financial history."],
        ["04", "Explain with sources", "Investigations cite collected evidence and acknowledge missing observations."],
      ].map(([number, title, copy]) => <article key={number}><span className="capability-number">{number} /</span><h3>{title}</h3><p>{copy}</p></article>)}</div><div className="architecture-note"><span className="architecture-symbol" aria-hidden="true">⌘</span><p><strong>Distributed behaviour. A compact demo footprint.</strong> Independent Go services and simulated banks share one free demo host, backed by PostgreSQL. Sleeping, shared-host limits and unavailable evidence stay visible.</p><a href="https://github.com/whoevenisshubham/TransactX">Explore the architecture <Icon name="arrow" /></a></div></div></section>
      <section className="landing-closing public-container"><p className="eyebrow">Follow a payment from the inside</p><h2>Less guessing.<br /><span>More understanding.</span></h2><a className="button button-primary" href="/demo">Choose a demo role <Icon name="arrow" /></a></section>
    </main><footer className="public-footer public-container"><a href="/" className="brand-lockup"><BrandMark />TransactX</a><p>A simulated payment network. No real money moves.</p><nav aria-label="Footer"><a href="https://github.com/whoevenisshubham/TransactX">GitHub</a><a href="https://github.com/whoevenisshubham/TransactX/tree/codex/render-neon-demo/docs/runbooks">Runbooks</a><a href="/privacy">Privacy</a></nav></footer>
  </div>;
}
