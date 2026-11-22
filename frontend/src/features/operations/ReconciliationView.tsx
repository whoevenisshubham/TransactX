import React, { useEffect, useRef, useState } from "react";
import { api } from "../../api";
import { Button, InlineError, PageHeader } from "../../components";

export function ReconciliationView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [runs, setRuns] = useState<any[]>([]);
  const [selected, setSelected] = useState<any>(null);
  const [discrepancies, setDiscrepancies] = useState<any[]>([]);
  const [offset, setOffset] = useState(0);
  const [limit] = useState(50);
  const [totalDiscrepancies, setTotalDiscrepancies] = useState<number | null>(null);
  const [nextOffset, setNextOffset] = useState<number | null>(null);
  const [participantId, setParticipantId] = useState("");
  const [scopeFrom, setScopeFrom] = useState("");
  const [scopeTo, setScopeTo] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function refresh() {
    try {
      const page = await api.opsReconciliationRuns(token);
      setRuns(page.items ?? []);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load reconciliation history.");
    }
  }

  useEffect(() => {
    void refresh();
  }, [token, liveRevision]);

  const discrepancyGeneration = useRef(0);

  async function loadDiscrepancies(runId: string, pageOffset: number) {
    const current = ++discrepancyGeneration.current;
    setOffset(pageOffset);
    try {
      const page = await api.opsReconciliationDiscrepancies(runId, token, limit, pageOffset);
      if (current === discrepancyGeneration.current) {
        setDiscrepancies(page.items ?? []);
        setTotalDiscrepancies(typeof page.total === "number" ? page.total : (page.items ? page.items.length : 0));
        setNextOffset(page.nextOffset ?? null);
        setError("");
      }
    } catch (err) {
      if (current === discrepancyGeneration.current) {
        setError(err instanceof Error ? err.message : "Could not load discrepancy evidence.");
      }
    }
  }

  async function choose(run: any) {
    setSelected(run);
    setDiscrepancies([]);
    setTotalDiscrepancies(null);
    setNextOffset(null);
    await loadDiscrepancies(run.id, 0);
  }

  async function execute(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const run = await api.opsReconciliationCreateRun({ participantId, scopeFrom, scopeTo }, token);
      await refresh();
      await choose(run);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Reconciliation failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Reconciliation"
        title="Reconciliation Runs"
        description="Compare central and participant ledger commitments over the same UTC scope."
      />

      <div className="console-card">
        <h3>Run reconciliation</h3>
        <form className="admin-form" onSubmit={execute}>
          <div className="form-group">
            <label htmlFor="operator-participant-id-1">Participant ID</label>
            <input
              id="operator-participant-id-1"
              value={participantId}
              onChange={(e) => setParticipantId(e.target.value)}
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="operator-scope-from-iso8601-2">Scope From (ISO8601)</label>
            <input
              id="operator-scope-from-iso8601-2"
              value={scopeFrom}
              onChange={(e) => setScopeFrom(e.target.value)}
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="operator-scope-to-iso8601-3">Scope To (ISO8601)</label>
            <input
              id="operator-scope-to-iso8601-3"
              value={scopeTo}
              onChange={(e) => setScopeTo(e.target.value)}
              required
            />
          </div>
          <Button type="submit" disabled={busy}>
            {busy ? "Running..." : "Run reconciliation"}
          </Button>
        </form>
      </div>

      {error && <InlineError message={error} />}

      <div className="console-card">
        <h3>Recorded runs</h3>
        <Button variant="secondary" onClick={refresh}>Refresh</Button>
        {runs.length === 0 && <p>No reconciliation runs recorded.</p>}
        {runs.length > 0 && (
          <div className="console-table-wrap" tabIndex={0} role="region" aria-label="Operational evidence table">
            <table className="console-table">
              <thead>
                <tr>
                  <th>Started</th>
                  <th>Participant</th>
                  <th>Status</th>
                  <th>Discrepancies</th>
                  <th>Detail</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => (
                  <tr key={run.id}>
                    <td>{run.startedAt}</td>
                    <td>{run.participantId}</td>
                    <td>{run.status}</td>
                    <td>{run.discrepancyCount}</td>
                    <td>
                      <button type="button" className="text-link" onClick={() => choose(run)}>
                        View
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {selected && (
        <div className="console-card">
          <h3>Run {selected.id}</h3>
          <p>Scope: {selected.scopeFrom} to {selected.scopeTo}</p>
          <p>Records: {selected.recordCount} · Status: {selected.status} · Discrepancies: {selected.discrepancyCount}</p>
          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Canonical root (base64)</span>
              <span className="console-spec-val mono" style={{ wordBreak: "break-all" }}>
                {selected.canonicalRoot ?? "Unavailable"}
              </span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Participant root (base64)</span>
              <span className="console-spec-val mono" style={{ wordBreak: "break-all" }}>
                {selected.participantRoot ?? "Unavailable"}
              </span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Commitment versions</span>
              <span className="console-spec-val">
                {selected.canonicalVersion ?? "—"} · {selected.algorithmVersion ?? "—"}
              </span>
            </div>
          </div>
          <div className="console-spec-grid">
            <div className="console-spec-item">
              <span>Elapsed</span>
              <strong>{(selected.elapsedNs / 1_000_000).toFixed(2)} ms</strong>
            </div>
            <div className="console-spec-item">
              <span>Merkle nodes visited</span>
              <strong>{selected.nodesVisited}</strong>
            </div>
            <div className="console-spec-item">
              <span>Records inspected</span>
              <strong>{selected.recordsInspected}</strong>
            </div>
            <div className="console-spec-item">
              <span>Bytes examined</span>
              <strong>{selected.bytesExamined}</strong>
            </div>
            <div className="console-spec-item">
              <span>Divergent buckets</span>
              <strong>{selected.divergentBuckets}</strong>
            </div>
            <div className="console-spec-item">
              <span>Divergent records</span>
              <strong>{selected.divergentRecords}</strong>
            </div>
          </div>
          {selected.errorMessage && <InlineError message={selected.errorMessage} />}
          {discrepancies.length === 0 ? (
            <div>
              <p>No discrepancy evidence in this page.</p>
              {offset > 0 && (
                <div className="flex gap-2 mt-4 items-center">
                  <Button
                    type="button"
                    variant="secondary"
                    size="sm"
                    onClick={() => selected && void loadDiscrepancies(selected.id, Math.max(0, offset - limit))}
                  >
                    Previous
                  </Button>
                </div>
              )}
            </div>
          ) : (
            <>
              {totalDiscrepancies !== null && (
                <p className="text-sm text-muted-foreground mb-2">
                  Showing {offset + 1}–{offset + discrepancies.length} of {totalDiscrepancies} discrepancies
                </p>
              )}
              {discrepancies.map((item, index) => (
                <div className="console-spec-item" key={item.id ?? index}>
                  <span>{item.mismatchCategory} · {item.bucketKey}</span>
                  <span>{item.evidence?.operation_id ?? "bucket-level"}</span>
                </div>
              ))}
              <div className="flex gap-2 mt-4 items-center">
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={offset === 0}
                  onClick={() => selected && void loadDiscrepancies(selected.id, Math.max(0, offset - limit))}
                >
                  Previous
                </Button>
                <Button
                  type="button"
                  variant="secondary"
                  size="sm"
                  disabled={nextOffset === null}
                  onClick={() => selected && nextOffset !== null && void loadDiscrepancies(selected.id, nextOffset)}
                >
                  Next
                </Button>
              </div>
            </>
          )}
        </div>
      )}
    </>
  );
}
