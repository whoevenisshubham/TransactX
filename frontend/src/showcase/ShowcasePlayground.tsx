import React, { useState } from "react";
import { Waves, Sparkles, Box, Cpu, RotateCcw } from "lucide-react";
import {
  NetworkCanvas,
  SpotlightCard,
  DotSentinel,
  ScrambleText,
  emitCelebrationBurst,
  useAudioEngine,
  type NetworkSimulationMode,
  type SentinelState,
} from "../components/effects";

export function ShowcasePlayground() {
  const [canvasMode, setCanvasMode] = useState<NetworkSimulationMode>("healthy");
  const [selectedNode, setSelectedNode] = useState("switch");
  const [sentinelState, setSentinelState] = useState<SentinelState>("probing");
  const [prismAngle, setPrismAngle] = useState({ rx: 12, ry: -18 });
  const [digestKey, setDigestKey] = useState(0);
  const { sfxEnabled, ambientEnabled, toggleSfx, toggleAmbient, play } = useAudioEngine();

  return (
    <section
      aria-labelledby="interactive-lab-heading"
      className="mt-10 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-6 sm:p-8 shadow-[var(--shadow-raised)] space-y-8"
    >
      <div className="flex flex-col md:flex-row md:items-end justify-between gap-4 border-b border-[var(--border-hairline)] pb-5">
        <div className="space-y-1.5">
          <div className="inline-flex items-center gap-2 font-mono text-xs font-semibold text-[var(--accent-strong)]">
            <Sparkles className="w-3.5 h-3.5" aria-hidden="true" />
            <span>Interactive Systems & Motion Playground</span>
          </div>
          <h2 id="interactive-lab-heading" className="font-display text-2xl font-bold tracking-tight text-[var(--text-primary)] m-0">
            Explore network telemetry, sound synthesis & 3D artifacts
          </h2>
          <p className="text-xs text-[var(--text-secondary)] m-0 max-w-2xl">
            Client-side experimental sandbox illustrating packet physics, procedural Web Audio cues, and cryptographic state indicators without affecting live ledgers.
          </p>
        </div>

        <button
          type="button"
          onClick={() => {
            emitCelebrationBurst();
            play("success");
          }}
          className="inline-flex items-center gap-2 rounded-lg bg-[var(--action-primary-bg)] px-3.5 py-2 text-xs font-semibold !text-[var(--action-primary-fg)] hover:opacity-90 cursor-pointer shrink-0"
        >
          <Sparkles className="w-3.5 h-3.5" aria-hidden="true" />
          <span>Trigger confirmation burst</span>
        </button>
      </div>

      {/* 1. Interactive Particle Network Canvas */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        <div className="lg:col-span-8 rounded-xl border border-[var(--border-hairline)] tx-blueprint-grid overflow-hidden">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border-hairline)] bg-[var(--bg-surface)]/85 px-3.5 py-2 text-xs">
            <span className="font-mono font-semibold text-[var(--text-primary)]">
              Particle Rail Topology · Selected: <code>{selectedNode}</code>
            </span>
            <div className="flex flex-wrap gap-1.5">
              {(["healthy", "wire-drop", "recovering", "overdrive"] as NetworkSimulationMode[]).map((m) => (
                <button
                  key={m}
                  type="button"
                  onClick={() => {
                    setCanvasMode(m);
                    play(m === "wire-drop" ? "error" : m === "overdrive" ? "success" : "tab");
                  }}
                  className={`rounded-md px-2 py-1 font-mono text-[11px] cursor-pointer border transition-colors ${
                    canvasMode === m
                      ? "border-[var(--action-accent-bg)] bg-[var(--status-info-bg)] text-[var(--status-info-fg)] font-semibold"
                      : "border-[var(--border-hairline)] bg-[var(--bg-surface)] text-[var(--text-secondary)]"
                  }`}
                >
                  {m}
                </button>
              ))}
            </div>
          </div>
          <NetworkCanvas mode={canvasMode} onNodeSelect={setSelectedNode} height={260} />
        </div>

        {/* 2. Web Audio Synth Lab */}
        <SpotlightCard
          tilt={false}
          className="lg:col-span-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 p-4 space-y-4"
        >
          <div className="relative z-10 space-y-4">
            <div className="flex items-center justify-between">
              <span className="inline-flex items-center gap-1.5 font-mono text-xs font-bold text-[var(--text-primary)]">
                <Waves className="w-4 h-4 text-[var(--accent-strong)]" aria-hidden="true" />
                <span>Procedural Web Audio</span>
              </span>
              <button
                type="button"
                aria-pressed={sfxEnabled}
                onClick={toggleSfx}
                className="rounded-md border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-2 py-1 text-[11px] font-mono text-[var(--text-primary)] cursor-pointer"
              >
                {sfxEnabled ? "Mute SFX" : "Enable SFX"}
              </button>
            </div>

            <p className="text-xs text-[var(--text-secondary)] m-0 leading-relaxed">
              Synthesized live via Web Audio oscillators (off by default). Enable SFX to audition state cues:
            </p>

            <div className="grid grid-cols-2 gap-2">
              {[
                { label: "Tick", cue: "tick" as const },
                { label: "Packet Ping", cue: "packet" as const },
                { label: "Settled Chime", cue: "success" as const },
                { label: "Wire Thud", cue: "error" as const },
              ].map((btn) => (
                <button
                  key={btn.cue}
                  type="button"
                  onClick={() => {
                    if (!sfxEnabled) toggleSfx();
                    play(btn.cue);
                  }}
                  className="rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] px-2.5 py-2 text-xs font-mono text-[var(--text-primary)] hover:border-[var(--border-active)] cursor-pointer"
                >
                  {btn.label}
                </button>
              ))}
            </div>

            <button
              type="button"
              aria-pressed={ambientEnabled}
              onClick={toggleAmbient}
              className={`w-full rounded-lg border px-3 py-2 text-xs font-mono font-semibold cursor-pointer transition-colors ${
                ambientEnabled
                  ? "border-[var(--action-accent-bg)] bg-[var(--status-info-bg)] text-[var(--status-info-fg)]"
                  : "border-[var(--border-hairline)] bg-[var(--bg-surface)] text-[var(--text-secondary)]"
              }`}
            >
              {ambientEnabled ? "Stop Ambient Synthscape" : "Start Ambient Fintech Drone"}
            </button>
          </div>
        </SpotlightCard>
      </div>

      {/* 3. Dot-Matrix Sentinel + 3D Interactive Vault Artifact */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <SpotlightCard className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/40 p-5 space-y-4">
          <div className="relative z-10 space-y-3">
            <div className="flex items-center justify-between">
              <div className="inline-flex items-center gap-2 font-mono text-xs font-bold text-[var(--text-primary)]">
                <Cpu className="w-4 h-4 text-[var(--accent-strong)]" aria-hidden="true" />
                <span>Dot-Matrix Telemetry Sentinel</span>
              </div>
              <DotSentinel state={sentinelState} label={`Sentinel preview: ${sentinelState}`} />
            </div>

            <div className="flex flex-wrap gap-1.5">
              {(["idle", "probing", "success", "warning", "error"] as SentinelState[]).map((s) => (
                <button
                  key={s}
                  type="button"
                  onClick={() => {
                    setSentinelState(s);
                    setDigestKey((k) => k + 1);
                    play("tab");
                  }}
                  className={`rounded-md px-2.5 py-1 text-[11px] font-mono cursor-pointer border ${
                    sentinelState === s
                      ? "border-[var(--action-accent-bg)] bg-[var(--status-info-bg)] text-[var(--status-info-fg)] font-semibold"
                      : "border-[var(--border-hairline)] bg-[var(--bg-surface)] text-[var(--text-secondary)]"
                  }`}
                >
                  {s}
                </button>
              ))}
            </div>

            <div className="rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] p-3 font-mono text-[11px] text-[var(--text-secondary)]">
              <span className="text-[var(--text-tertiary)] block">Merkle Digest Scramble Probe</span>
              <ScrambleText
                key={digestKey}
                text={`TXNODE|v1|${sentinelState.toUpperCase()}|0x9F1A84C2`}
                className="font-bold text-[var(--text-primary)] block mt-1"
              />
            </div>
          </div>
        </SpotlightCard>

        {/* Interactive Perspective 3D Vault Card */}
        <div className="rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/40 p-5 flex flex-col justify-between space-y-4">
          <div className="flex items-center justify-between text-xs font-mono">
            <span className="inline-flex items-center gap-1.5 font-bold text-[var(--text-primary)]">
              <Box className="w-4 h-4 text-[var(--accent-strong)]" aria-hidden="true" />
              <span>3D Isometric Vault Cube</span>
            </span>
            <button
              type="button"
              onClick={() => setPrismAngle({ rx: 12, ry: -18 })}
              aria-label="Reset 3D orientation"
              className="inline-flex items-center gap-1 text-[11px] text-[var(--text-secondary)] hover:text-[var(--text-primary)] cursor-pointer"
            >
              <RotateCcw className="w-3 h-3" aria-hidden="true" />
              Reset
            </button>
          </div>

          <div
            data-testid="vault-prism-stage"
            className="flex items-center justify-center py-4"
            onPointerMove={(e) => {
              if (e.buttons !== 1 && e.pointerType === "mouse") return;
              const rect = e.currentTarget.getBoundingClientRect();
              const nx = ((e.clientX - rect.left) / rect.width - 0.5) * 34;
              const ny = ((e.clientY - rect.top) / rect.height - 0.5) * -28;
              setPrismAngle({ rx: ny, ry: nx });
            }}
          >
            <div
              data-testid="vault-prism-card"
              style={{
                transform: `perspective(800px) rotateX(${prismAngle.rx}deg) rotateY(${prismAngle.ry}deg)`,
              }}
              className="w-64 rounded-xl border border-[var(--action-accent-bg)]/50 bg-[#0b101f] p-4 text-[#f8fafc] shadow-[var(--shadow-modal)] transition-transform duration-150 select-none cursor-grab active:cursor-grabbing"
            >
              <div className="flex items-center justify-between text-[10px] font-mono text-[#93c5fd]">
                <span>TRANSACTX ENGINE</span>
                <span>DOUBLE-ENTRY</span>
              </div>
              <div className="mt-2 font-mono text-lg font-bold tracking-tight">
                SQL SUM = 0
              </div>
              <div className="mt-1 text-[11px] font-mono text-[#cbd5e1]">
                Drag pointer across frame to orbit 3D view
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
