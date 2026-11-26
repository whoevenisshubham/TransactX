import { Amount, Button, formatDate, StatusBadge } from "../../components";
import type { OfflineQueuePanelProps } from "./types";

export function OfflineQueuePanel({ intents, onRetry }: OfflineQueuePanelProps) {
  if (intents.length === 0) return null;

  return (
    <section className="offline-queue-panel rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-5 space-y-3">
      <div className="section-heading">
        <div>
          <p className="eyebrow">Local sync</p>
          <h2>Offline payment intents</h2>
        </div>
        <span className="toolbar-note font-mono tabular-nums">{intents.length} retained on device</span>
      </div>

      <div className="offline-intent-list">
        {intents.map((intent) => (
          <div className="offline-intent-row" key={intent.clientRequestId}>
            <div>
              <strong>{intent.payload.recipient}</strong>
              <small>
                <Amount paise={intent.payload.amountPaise} /> · {formatDate(intent.createdAt)}
              </small>
            </div>
            <div className="offline-intent-status">
              <StatusBadge
                state={intent.replay?.resolution === "PENDING" ? "PENDING" : intent.state}
              />
              {intent.state === "FAILED" && (
                <Button
                  variant="secondary"
                  onClick={() => void onRetry(intent.clientRequestId)}
                >
                  Retry safely
                </Button>
              )}
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
