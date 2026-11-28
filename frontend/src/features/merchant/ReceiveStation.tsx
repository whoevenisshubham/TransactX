import { useEffect, useState } from "react";
import { api } from "../../api";
import { Button, Icon, InlineError, PageHeader, Skeleton } from "../../components";
import { useAudioEngine } from "../../components/effects";
import { CopyValue } from "../../shared/CopyValue";
import { toast } from "sonner";
import { DetailsSkeleton } from "../../shared/ShellStates";
import { encodeQRSvg } from "../../qr";
import { downloadQR } from "../../merchant/downloadQR";
import type { MerchantReceiveInfo } from "./types";

export interface ReceiveStationProps {
  token: string;
}

export function ReceiveStation({ token }: ReceiveStationProps) {
  const [info, setInfo] = useState<MerchantReceiveInfo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [qrSvg, setQrSvg] = useState("");
  const [downloadError, setDownloadError] = useState("");
  const { play } = useAudioEngine();

  useEffect(() => {
    let active = true;
    api.merchantReceiveInfo(token)
      .then((data) => {
        if (!active) return;
        setInfo(data);
        return encodeQRSvg(data.paymentIdentifier).then((svg) => {
          if (active) setQrSvg(svg);
        });
      })
      .catch((caught) => {
        if (active) {
          setError(
            caught instanceof Error ? caught.message : "Could not load receive information."
          );
        }
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [token]);

  if (loading) return <DetailsSkeleton />;

  if (error || !info) {
    return (
      <div className="center-state">
        <InlineError message={error || "Receive information unavailable."} />
        <Button variant="secondary" onClick={() => window.location.reload()}>
          Try again
        </Button>
      </div>
    );
  }

  if (info.accountStatus !== "ACTIVE") {
    return (
      <div className="center-state">
        <div className="result-mark result-pending">
          <Icon name="alert" size={24} />
        </div>
        <h1 style={{ fontSize: "1.6rem" }}>Account inactive</h1>
        <p style={{ color: "var(--muted)" }}>
          Your merchant account is not currently active. Contact support for assistance.
        </p>
      </div>
    );
  }

  return (
    <>
      <PageHeader
        eyebrow="Receive Payments"
        title="Accept payments"
        description="Share your QR code or payment identifier to accept payments from customers on TransactX."
      />

      <div className="merchant-receive-layout">
        <div className="merchant-qr-card">
          <span className="merchant-network-label">TransactX · Simulated payments</span>
          <div className="merchant-qr-label">
            <span className="eyebrow">Scan to pay</span>
            <p className="merchant-qr-hint">
              Share this identifier with a TransactX customer. This is not a UPI or bank-app QR.
            </p>
          </div>

          {qrSvg ? (
            <div
              className="merchant-qr-frame bg-white text-[#0f172a] p-3 rounded-xl"
              role="img"
              aria-label={`QR code containing payment ID ${info.paymentIdentifier}`}
              dangerouslySetInnerHTML={{ __html: qrSvg }}
            />
          ) : (
            <div className="merchant-qr-frame merchant-qr-placeholder">
              <Skeleton className="skeleton-block" />
              <span>Generating QR code…</span>
            </div>
          )}

          <div className="merchant-qr-id">
            <span className="eyebrow">Payment ID</span>
            <strong className="merchant-pid">{info.paymentIdentifier}</strong>
          </div>

          <CopyValue value={info.paymentIdentifier} label="Copy payment ID" />

          <div className="merchant-qr-downloads">
            {(["svg", "png"] as const).map((format) => (
              <Button
                variant="secondary"
                disabled={!qrSvg}
                key={format}
                onClick={() => {
                  setDownloadError("");
                  play("packet");
                  void downloadQR(qrSvg, format)
                    .then(() => toast.success(`Merchant QR ${format.toUpperCase()} downloaded`))
                    .catch(() =>
                      setDownloadError("Download unavailable. Please try again.")
                    );
                }}
              >
                Download {format.toUpperCase()}
              </Button>
            ))}
          </div>
          {downloadError && <InlineError message={downloadError} />}
        </div>

        <div className="merchant-receive-instructions">
          <div className="merchant-receive-note">
            <span className="note-index">01</span>
            <div>
              <strong>Verified recipient</strong>
              <p>
                A QR scanner can read your payment ID. The customer can enter that ID in TransactX and review the resolved recipient before confirming.
              </p>
            </div>
          </div>
          <div className="merchant-receive-note">
            <span className="note-index">02</span>
            <div>
              <strong>Follow the outcome</strong>
              <p>
                Only a completed payment is confirmed. A pending confirmation can still settle; check its original receipt before asking for another payment.
              </p>
            </div>
          </div>
          <div className="merchant-receive-note">
            <span className="note-index">03</span>
            <div>
              <strong>Account verification</strong>
              <p>
                Active merchant account. Associated account number:{" "}
                <strong className="mono-code">{info.accountNumber}</strong>
              </p>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
