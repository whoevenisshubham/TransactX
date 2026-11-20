import React, { useState } from "react";
import { BrandMark, Icon } from "../../components";

const productPreviews = import.meta.glob("../../assets/product/*.webp", {
  eager: true,
  query: "?url",
  import: "default",
}) as Record<string, string>;

export const roles = [
  {
    id: "customer",
    name: "Customer",
    title: "Your money. A clearer view.",
    text: "Review a recipient, send a simulated payment and keep its receipt. Your account stays your own.",
    action: "Open customer demo",
  },
  {
    id: "merchant",
    name: "Merchant",
    title: "A desk built for receiving.",
    text: "Share your payment identity, follow incoming payments and separate completed receipts from uncertain outcomes.",
    action: "Open merchant demo",
  },
  {
    id: "operator",
    name: "Operations",
    title: "Follow the facts, not the guesswork.",
    text: "Read a payment’s original path, compare independent bank observations and open an investigation with sources.",
    action: "Open operations demo",
  },
];

function RolePreview({ role }: { role: string }) {
  const descriptions: Record<string, string> = {
    customer: "Customer account document with exact balance, Send action and bank identity",
    merchant: "Merchant receiving desk with identifier QR and copy/download controls",
    operator: "Operations payment journey report with grounded conclusion and exact evidence citation",
  };
  const dimensions: Record<string, [number, number]> = {
    customer: [350, 342],
    merchant: [373, 445],
    operator: [470, 270],
  };
  const captions: Record<string, string> = {
    customer: "Balance · Send · Bank identity",
    merchant: "QR · Payment identity · Download",
    operator: "Grounded conclusion · Exact sources",
  };

  return (
    <figure className="role-product-preview">
      <div className="preview-window-bar">
        <span>
          <BrandMark /> TransactX
        </span>
        <span>Captured UI · synthetic fixtures</span>
      </div>
      {["light", "dark"].map((theme) => (
        <a
          className={`preview-capture preview-capture-${theme}`}
          key={theme}
          href={productPreviews[`../../assets/product/${role}-${theme}.webp`]}
          target="_blank"
          rel="noreferrer"
          aria-label={`Open full-size ${descriptions[role]}`}
        >
          <img
            src={productPreviews[`../../assets/product/${role}-${theme}.webp`]}
            width={dimensions[role][0]}
            height={dimensions[role][1]}
            loading="lazy"
            alt={descriptions[role]}
          />
        </a>
      ))}
      <figcaption>
        {captions[role]}
        <span>Open image to inspect ↗</span>
      </figcaption>
    </figure>
  );
}

export function WorkspaceExplorer() {
  const [selected, setSelected] = useState(0);
  const role = roles[selected];

  return (
    <section id="workspaces" className="reset-role-section public-container">
      <div className="reset-section-heading">
        <p className="eyebrow">ONE NETWORK. YOUR PERSPECTIVE.</p>
        <h2>Choose your view. Follow a payment.</h2>
      </div>
      <div className="reset-role-explorer">
        <div className="role-explorer-copy">
          <div role="tablist" aria-label="Explore workspaces">
            {roles.map((item, index) => (
              <button
                key={item.id}
                role="tab"
                id={`role-tab-${item.id}`}
                aria-selected={selected === index}
                aria-controls="role-preview-panel"
                tabIndex={selected === index ? 0 : -1}
                onClick={() => setSelected(index)}
                onKeyDown={(event) => {
                  if (["ArrowRight", "ArrowLeft", "Home", "End"].includes(event.key)) {
                    event.preventDefault();
                    const next =
                      event.key === "Home"
                        ? 0
                        : event.key === "End"
                        ? 2
                        : (selected + (event.key === "ArrowRight" ? 1 : 2)) % 3;
                    setSelected(next);
                    document.getElementById(`role-tab-${roles[next].id}`)?.focus();
                  }
                }}
              >
                {item.name}
              </button>
            ))}
          </div>
          <h3>{role.title}</h3>
          <p>{role.text}</p>
          <a className="reset-launch-link" href={`/demo?role=${role.id}`}>
            {role.action}
            <Icon name="arrow" />
          </a>
          <span className="role-scope-note">Shared synthetic accounts · No registration</span>
        </div>
        <div
          id="role-preview-panel"
          role="tabpanel"
          tabIndex={0}
          aria-labelledby={`role-tab-${role.id}`}
        >
          <RolePreview role={role.id} />
        </div>
      </div>
    </section>
  );
}
