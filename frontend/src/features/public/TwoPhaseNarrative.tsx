import React, { useState } from "react";
import { Icon } from "../../components";

export const narrative = [
  {
    title: "An intent is saved.",
    label: "Original request",
    state: "Recorded",
    copy: "One payment identity follows the request, including a retry. A saved intent is not settled money.",
    a: "Not yet observed",
    b: "Not yet observed",
    outcome: "Awaiting observations",
  },
  {
    title: "A reply goes missing.",
    label: "Independent observations",
    state: "Unknown outcome",
    copy: "A bank may have accepted an operation even when its reply is lost. The system checks the original operation; it does not invent a failure.",
    a: "Hold reported",
    b: "Reply unavailable",
    outcome: "Pending reconciliation",
  },
  {
    title: "The facts come together.",
    label: "Collected evidence",
    state: "Verified in this example",
    copy: "After the participant outcome is established, the central record completes. The explanation cites what was actually collected.",
    a: "Source confirmed",
    b: "Credit confirmed",
    outcome: "Completed with evidence",
  },
];

export function TwoPhaseNarrative() {
  const [step, setStep] = useState(1);
  const moment = narrative[step];

  return (
    <section id="how-it-works" className="reset-narrative">
      <div className="public-container">
        <div className="reset-section-heading">
          <p className="eyebrow">A PAYMENT IS MORE THAN A RESPONSE.</p>
          <h2>
            When the connection breaks,<br />
            keep the story intact.
          </h2>
        </div>
        <div className="narrative-layout">
          <div className="narrative-controls">
            <span className="example-label">Illustrative scenario · not live observations</span>
            {narrative.map((item, index) => (
              <button
                key={item.title}
                type="button"
                aria-pressed={step === index}
                onClick={() => setStep(index)}
              >
                <span>0{index + 1}</span>
                <strong>{item.title}</strong>
                <Icon name="arrow" />
              </button>
            ))}
            <p>{moment.copy}</p>
          </div>
          <div className="narrative-stage" aria-live="polite">
            <div className="narrative-stage-label">
              <span>{moment.label}</span>
              <span>{moment.state}</span>
            </div>
            <div className="narrative-banks">
              <div>
                <span>A</span>
                <strong>Bank A</strong>
                <small>{moment.a}</small>
              </div>
              <div className="narrative-connector" aria-hidden="true">
                →
              </div>
              <div>
                <span>B</span>
                <strong>Bank B</strong>
                <small>{moment.b}</small>
              </div>
            </div>
            <div className={`narrative-outcome narrative-outcome-${step}`}>
              <span>Central payment</span>
              <strong>{moment.outcome}</strong>
              <small>Bank observations and the authoritative outcome are distinct.</small>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
