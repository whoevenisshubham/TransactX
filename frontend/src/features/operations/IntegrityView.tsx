import React, { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../../api";
import { Button, InlineError, PageHeader } from "../../components";
import type {
  IntegrityCheckResult,
  IntegrityProof,
  IntegrityRunResult,
  IntegrityVerifyProofResult,
} from "./types";

export function IntegrityView({ token, liveRevision }: { token: string; liveRevision: number }) {
  const [integrityRuns, setIntegrityRuns] = useState<IntegrityRunResult[]>([]);
  const [integrityDetail, setIntegrityDetail] = useState<IntegrityRunResult | null>(null);
  const [integrityError, setIntegrityError] = useState("");
  const [checking, setChecking] = useState(false);
  const [operationId, setOperationId] = useState("");
  const [participantId, setParticipantId] = useState("");
  const [scopeFrom, setScopeFrom] = useState("");
  const [scopeTo, setScopeTo] = useState("");

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [proof, setProof] = useState<IntegrityProof | null>(null);

  const [verifying, setVerifying] = useState(false);
  const [verifyResult, setVerifyResult] = useState<IntegrityVerifyProofResult | null>(null);
  const [verifyError, setVerifyError] = useState("");
  const selectedRunIdRef = useRef<string | null>(null);
  const proofParamsRef = useRef<{ participantId: string; scopeFrom: string; scopeTo: string; operationId: string } | null>(null);
  const proofGenRef = useRef(0);
  const verifyGenRef = useRef(0);
  const detailGenRef = useRef(0);

  async function loadIntegrity() {
    try {
      const status = await api.opsIntegrityStatus(token);
      setIntegrityRuns(status.runs ?? []);
      const targetRunId = selectedRunIdRef.current && status.runs?.some((r) => r.runId === selectedRunIdRef.current)
        ? selectedRunIdRef.current
        : status.runs?.[0]?.runId;
      if (targetRunId) {
        selectedRunIdRef.current = targetRunId;
        const currentGen = ++detailGenRef.current;
        const detail = await api.opsIntegrityRun(targetRunId, token);
        if (currentGen === detailGenRef.current) {
          setIntegrityDetail(detail);
        }
      }
      setIntegrityError("");
    } catch (err) {
      setIntegrityError(err instanceof Error ? err.message : "Could not load integrity state.");
    }
  }

  useEffect(() => {
    void loadIntegrity();
  }, [token, liveRevision]);

  async function runIntegrity() {
    setChecking(true);
    setIntegrityError("");
    const hasPart = Boolean(participantId.trim());
    const hasFrom = Boolean(scopeFrom);
    const hasTo = Boolean(scopeTo);
    if ((hasPart || hasFrom || hasTo) && !(hasPart && hasFrom && hasTo)) {
      setIntegrityError("Participant ID, scope start, and scope end must all be provided together to verify Merkle consistency.");
      setChecking(false);
      return;
    }
    try {
      const input: Record<string, unknown> = {};
      if (hasPart && hasFrom && hasTo) {
        input.participantId = participantId.trim();
        input.scope = { from: scopeFrom, to: scopeTo };
      }
      const result = await api.opsIntegrityCheck(input, token);
      selectedRunIdRef.current = result.runId;
      setIntegrityDetail(result);
      await loadIntegrity();
    } catch (err) {
      setIntegrityError(err instanceof Error ? err.message : "Integrity check failed.");
    } finally {
      setChecking(false);
    }
  }

  async function fetchProof(e: React.FormEvent) {
    e.preventDefault();
    const currentProofGen = ++proofGenRef.current;
    verifyGenRef.current++;
    setLoading(true);
    setError("");
    setProof(null);
    setVerifyResult(null);
    setVerifyError("");
    try {
      const res = await api.opsReconciliationGetProof(operationId, participantId, scopeFrom, scopeTo, token);
      if (currentProofGen !== proofGenRef.current) return;
      proofParamsRef.current = { participantId, scopeFrom, scopeTo, operationId };
      setProof(res);
    } catch (err) {
      if (currentProofGen !== proofGenRef.current) return;
      if (err instanceof ApiError) setError(err.message);
      else setError("Failed to fetch integrity proof");
    } finally {
      if (currentProofGen === proofGenRef.current) {
        setLoading(false);
      }
    }
  }

  async function handleVerify() {
    const params = proofParamsRef.current;
    if (!proof || !params) return;
    const currentVerifyGen = ++verifyGenRef.current;
    const boundOpId = params.operationId;
    setVerifying(true);
    setVerifyError("");
    setVerifyResult(null);
    try {
      const res = await api.opsReconciliationVerifyProof(params.participantId, params.scopeFrom, params.scopeTo, proof, token);
      if (currentVerifyGen !== verifyGenRef.current) return;
      if (proofParamsRef.current?.operationId !== boundOpId) return;
      setVerifyResult(res);
    } catch (err) {
      if (currentVerifyGen !== verifyGenRef.current) return;
      if (err instanceof ApiError) setVerifyError(err.message);
      else setVerifyError("Failed to verify proof");
    } finally {
      if (currentVerifyGen === verifyGenRef.current) {
        setVerifying(false);
      }
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Invariant Verification"
        title="Ledger Integrity Engine"
        description="Run and review persisted financial invariant checks."
      />
      <div className="console-card space-y-3">
        <h3>Financial integrity</h3>
        <p className="ops-muted">Run the general checks, or fill participant and scope below to include Merkle commitment consistency.</p>
        <div className="flex flex-wrap gap-2">
          <Button onClick={runIntegrity} disabled={checking}>
            {checking ? "Checking..." : "Run integrity checks"}
          </Button>
          <Button variant="secondary" onClick={loadIntegrity}>Refresh history</Button>
        </div>
        {integrityError && <InlineError message={integrityError} />}
        {integrityRuns.length === 0 && <p>No integrity checks recorded.</p>}
        {integrityRuns.map((run) => (
          <button
            type="button"
            className="text-link"
            key={run.runId}
            onClick={async () => {
              const currentGen = ++detailGenRef.current;
              try {
                selectedRunIdRef.current = run.runId;
                const detail = await api.opsIntegrityRun(run.runId, token);
                if (currentGen === detailGenRef.current) {
                  setIntegrityDetail(detail);
                  setIntegrityError("");
                }
              } catch (err) {
                if (currentGen === detailGenRef.current) {
                  setIntegrityError(err instanceof Error ? err.message : "Could not load integrity run.");
                }
              }
            }}
            style={{ display: "block", marginTop: "0.8rem" }}
          >
            {run.startedAt} · {run.status} · {run.summary.failed} failed · {run.summary.errors} errors
          </button>
        ))}
        {integrityDetail && (
          <div>
            <h4>Run {integrityDetail.runId}</h4>
            {(integrityDetail.checks ?? []).map((check: IntegrityCheckResult) => (
              <div className="console-spec-item" key={check.code}>
                <span className="ops-mono font-semibold">{check.code}</span>
                <span className="flex flex-wrap items-center gap-2 text-xs">
                  <span
                    className={`console-pill ${
                      check.status === "PASS"
                        ? "console-pill-success"
                        : check.status === "FAIL" || check.status === "ERROR"
                        ? "console-pill-error"
                        : "console-pill-warning"
                    }`}
                  >
                    {check.status}
                  </span>
                  <span>{check.message}</span>
                </span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="console-card">
        <h3>Fetch Integrity Proof</h3>
        <form className="admin-form" onSubmit={fetchProof}>
          <div className="form-group">
            <label htmlFor="operator-operation-id-7">Operation ID</label>
            <input
              id="operator-operation-id-7"
              value={operationId}
              onChange={(e) => setOperationId(e.target.value)}
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="operator-participant-id-8">Participant ID</label>
            <input
              id="operator-participant-id-8"
              value={participantId}
              onChange={(e) => setParticipantId(e.target.value)}
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="operator-scope-from-iso8601-9">Scope From (ISO8601)</label>
            <input
              id="operator-scope-from-iso8601-9"
              value={scopeFrom}
              onChange={(e) => setScopeFrom(e.target.value)}
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="operator-scope-to-iso8601-10">Scope To (ISO8601)</label>
            <input
              id="operator-scope-to-iso8601-10"
              value={scopeTo}
              onChange={(e) => setScopeTo(e.target.value)}
              required
            />
          </div>
          <div className="form-actions">
            <Button type="submit" disabled={loading}>Fetch Proof</Button>
          </div>
        </form>
        {error && <InlineError message={error} />}
      </div>

      {proof && (
        <div className="console-card">
          <div className="integrity-proof-header">
            <h3>Integrity Proof Result</h3>
            <Button variant="secondary" onClick={handleVerify} disabled={verifying}>
              {verifying ? "Verifying..." : "Verify Proof Against Trusted State"}
            </Button>
          </div>

          {verifyError && <InlineError message={verifyError} />}
          {verifyResult && (
            <div
              className="console-notice-box"
              style={{
                marginBottom: "1.5rem",
                padding: "1rem",
                borderColor: verifyResult.valid ? "var(--success)" : "var(--error)",
                color: verifyResult.valid ? "var(--success)" : "var(--error)",
              }}
            >
              <h4 style={{ margin: "0 0 0.5rem 0" }}>
                {verifyResult.valid ? "VERIFICATION SUCCESSFUL" : "VERIFICATION FAILED"}
              </h4>
              <p className="integrity-reconstructed-root" style={{ margin: 0, fontSize: "0.85rem" }}>
                Reconstructed Root: <span style={{ fontFamily: "monospace" }}>{verifyResult.reconstructedRoot}</span>
              </p>
            </div>
          )}

          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Leaf Hash</span>
              <span className="console-spec-val" style={{ fontFamily: "monospace" }}>{proof.leafHashHex}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Generation</span>
              <span className="console-spec-val">{proof.generation}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Expected Root</span>
              <span className="console-spec-val" style={{ fontFamily: "monospace" }}>{proof.expectedRootHex}</span>
            </div>
          </div>

          <h4>Bucket Path ({proof.bucketPath?.length || 0} nodes)</h4>
          {proof.bucketPath?.length > 0 ? (
            <ul style={{ fontFamily: "monospace", fontSize: "0.85rem", listStyle: "none", paddingLeft: 0 }}>
              {proof.bucketPath.map((p, i) => (
                <li key={i} style={{ marginBottom: "0.25rem" }}>
                  <strong>{p.position}</strong>: {p.hashHex || "PROMOTED"}
                </li>
              ))}
            </ul>
          ) : (
            <p>No bucket path needed.</p>
          )}

          <h4>Global Path ({proof.globalPath?.length || 0} nodes)</h4>
          {proof.globalPath?.length > 0 ? (
            <ul style={{ fontFamily: "monospace", fontSize: "0.85rem", listStyle: "none", paddingLeft: 0 }}>
              {proof.globalPath.map((p, i) => (
                <li key={i} style={{ marginBottom: "0.25rem" }}>
                  <strong>{p.position}</strong>: {p.hashHex || "PROMOTED"}
                </li>
              ))}
            </ul>
          ) : (
            <p>No global path needed.</p>
          )}

          <h4>Generation Metrics</h4>
          <div className="console-spec-list">
            <div className="console-spec-item">
              <span className="console-spec-label">Duration (ns)</span>
              <span className="console-spec-val">{proof.generationMetrics.durationNs}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Bucket Siblings</span>
              <span className="console-spec-val">{proof.generationMetrics.bucketSiblingCount}</span>
            </div>
            <div className="console-spec-item">
              <span className="console-spec-label">Global Siblings</span>
              <span className="console-spec-val">{proof.generationMetrics.globalSiblingCount}</span>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
