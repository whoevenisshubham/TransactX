import React from "react";
import { BrandMark, Icon } from "../../components";

export function PaymentScene() {
  return (
    <div className="payment-scene" aria-label="Illustrative payment receipt, bank path and cited investigation">
      <div className="scene-grid" aria-hidden="true" />
      <div className="scene-caption">
        <span>The payment, opened up</span>
        <span>Illustrative example</span>
      </div>
      <div className="scene-path">
        <span>
          <i>A</i>Bank A<small>Sender</small>
        </span>
        <b aria-hidden="true">⟶</b>
        <span className="scene-path-center">
          <BrandMark />
          <small>Recorded outcome</small>
        </span>
        <b aria-hidden="true">⟶</b>
        <span>
          <i>B</i>Bank B<small>Recipient</small>
        </span>
      </div>
      <div className="scene-document">
        <div className="scene-receipt">
          <div className="receipt-stamp">
            <Icon name="check" size={23} />
          </div>
          <p>Payment receipt</p>
          <strong>₹1,250<span>.00</span></strong>
          <span className="scene-complete">Completed</span>
          <dl>
            <div>
              <dt>From</dt>
              <dd>Alice · Bank A</dd>
            </div>
            <div>
              <dt>To</dt>
              <dd>Studio Store · Bank B</dd>
            </div>
          </dl>
          <footer>
            <BrandMark />
            <span>Original payment. One reference.</span>
          </footer>
        </div>
        <div className="scene-annotation">
          <span className="annotation-star" aria-hidden="true">✳</span>
          <p className="section-kicker">Cited investigation</p>
          <h2>The outcome.<br />With its sources.</h2>
          <p>Central outcome and bank observations stay distinct.</p>
          <span className="scene-citation"><span>↗</span> Collected payment evidence</span>
          <small>Illustrative report · no provider request</small>
        </div>
      </div>
      <div className="scene-bottom">
        <span>INTENT</span>
        <span aria-hidden="true">────────</span>
        <span>OBSERVATIONS</span>
        <span aria-hidden="true">────────</span>
        <span>EVIDENCE</span>
      </div>
    </div>
  );
}
