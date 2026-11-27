import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { Volume2, VolumeX, Waves } from "lucide-react";

export type SoundCue = "tick" | "tab" | "success" | "error" | "packet" | "modal";

interface AudioEngineContextValue {
  sfxEnabled: boolean;
  ambientEnabled: boolean;
  volume: number;
  level: number;
  toggleSfx: () => void;
  toggleAmbient: () => void;
  setVolume: (v: number) => void;
  play: (cue: SoundCue) => void;
}

const AudioEngineContext = createContext<AudioEngineContextValue | null>(null);

const SFX_KEY = "transactx.audio.sfx";
const AMBIENT_KEY = "transactx.audio.ambient";

function readFlag(key: string): boolean {
  if (typeof window === "undefined") return false;
  try {
    return localStorage.getItem(key) === "true";
  } catch {
    return false;
  }
}

function writeFlag(key: string, val: boolean): void {
  if (typeof window === "undefined") return;
  try {
    localStorage.setItem(key, val ? "true" : "false");
  } catch {
    // Ignore storage access errors in restricted contexts
  }
}

export function AudioProvider({ children }: { children: React.ReactNode }) {
  // Always OFF by default — never autoplay sound without explicit user action
  const [sfxEnabled, setSfxEnabled] = useState<boolean>(() => readFlag(SFX_KEY));
  const [ambientEnabled, setAmbientEnabled] = useState<boolean>(false);
  const [volume, setVolumeState] = useState<number>(0.45);
  const [level, setLevel] = useState<number>(0);

  const ctxRef = useRef<AudioContext | null>(null);
  const ambientNodesRef = useRef<{
    master: GainNode;
    oscs: OscillatorNode[];
  } | null>(null);
  const decayTimerRef = useRef<number | null>(null);

  const ensureContext = useCallback((): AudioContext | null => {
    if (typeof window === "undefined") return null;
    const AudioCtx =
      window.AudioContext ||
      (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
    if (!AudioCtx) return null;
    if (!ctxRef.current) {
      ctxRef.current = new AudioCtx();
    }
    if (ctxRef.current.state === "suspended") {
      void ctxRef.current.resume().catch(() => undefined);
    }
    return ctxRef.current;
  }, []);

  const bumpLevel = useCallback((peak: number) => {
    setLevel((prev) => Math.max(prev, peak));
    if (decayTimerRef.current) window.clearTimeout(decayTimerRef.current);
    decayTimerRef.current = window.setTimeout(() => {
      setLevel(0);
    }, 240);
  }, []);

  const stopAmbient = useCallback(() => {
    const active = ambientNodesRef.current;
    const ctx = ctxRef.current;
    if (!active || !ctx) return;
    const now = ctx.currentTime;
    try {
      active.master.gain.cancelScheduledValues(now);
      active.master.gain.setTargetAtTime(0.0001, now, 0.12);
      const toStop = active.oscs;
      window.setTimeout(() => {
        for (const osc of toStop) {
          try {
            osc.stop();
            osc.disconnect();
          } catch {
            // already stopped
          }
        }
      }, 400);
    } catch {
      // ignore teardown race
    }
    ambientNodesRef.current = null;
  }, []);

  const startAmbient = useCallback(
    (vol: number) => {
      const ctx = ensureContext();
      if (!ctx) return;
      stopAmbient();
      const now = ctx.currentTime;
      const filter = ctx.createBiquadFilter();
      filter.type = "lowpass";
      filter.frequency.setValueAtTime(340, now);

      const master = ctx.createGain();
      master.gain.setValueAtTime(0.0001, now);
      master.gain.linearRampToValueAtTime(Math.max(0.01, vol * 0.045), now + 0.8);

      filter.connect(master);
      master.connect(ctx.destination);

      // Subtle warm F#m9 fintech ambient drone (110Hz, 164.81Hz, 246.94Hz)
      const freqs = [110, 164.81, 246.94];
      const oscs = freqs.map((freq, idx) => {
        const osc = ctx.createOscillator();
        osc.type = idx === 0 ? "sine" : "triangle";
        osc.frequency.setValueAtTime(freq, now);
        osc.detune.setValueAtTime((idx - 1) * 4, now);
        osc.connect(filter);
        osc.start(now);
        return osc;
      });

      ambientNodesRef.current = { master, oscs };
    },
    [ensureContext, stopAmbient]
  );

  useEffect(() => {
    if (ambientNodesRef.current && ctxRef.current) {
      const now = ctxRef.current.currentTime;
      ambientNodesRef.current.master.gain.setTargetAtTime(Math.max(0.005, volume * 0.045), now, 0.1);
    }
  }, [volume]);

  useEffect(() => {
    return () => {
      if (decayTimerRef.current) window.clearTimeout(decayTimerRef.current);
      stopAmbient();
      if (ctxRef.current) {
        void ctxRef.current.close().catch(() => undefined);
      }
    };
  }, [stopAmbient]);

  const play = useCallback(
    (cue: SoundCue) => {
      if (!sfxEnabled) return;
      const ctx = ensureContext();
      if (!ctx) return;
      const now = ctx.currentTime;
      const gainScale = Math.max(0.05, volume * 0.14);

      const playTone = (
        freq: number,
        endFreq: number,
        duration: number,
        type: OscillatorType,
        delay = 0,
        peakGain = gainScale
      ) => {
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.type = type;
        osc.frequency.setValueAtTime(freq, now + delay);
        osc.frequency.exponentialRampToValueAtTime(Math.max(40, endFreq), now + delay + duration);
        gain.gain.setValueAtTime(0.0001, now + delay);
        gain.gain.linearRampToValueAtTime(peakGain, now + delay + 0.012);
        gain.gain.exponentialRampToValueAtTime(0.0001, now + delay + duration);
        osc.connect(gain);
        gain.connect(ctx.destination);
        osc.start(now + delay);
        osc.stop(now + delay + duration + 0.02);
      };

      switch (cue) {
        case "tick":
          playTone(720, 420, 0.045, "sine", 0, gainScale * 0.45);
          bumpLevel(0.45);
          break;
        case "tab":
          playTone(540, 780, 0.075, "triangle", 0, gainScale * 0.55);
          bumpLevel(0.6);
          break;
        case "packet":
          playTone(880, 1174, 0.09, "sine", 0, gainScale * 0.4);
          bumpLevel(0.55);
          break;
        case "modal":
          playTone(330, 520, 0.12, "sine", 0, gainScale * 0.5);
          bumpLevel(0.65);
          break;
        case "success":
          playTone(523.25, 523.25, 0.14, "sine", 0, gainScale * 0.7);
          playTone(659.25, 659.25, 0.16, "sine", 0.06, gainScale * 0.7);
          playTone(783.99, 783.99, 0.24, "triangle", 0.12, gainScale * 0.8);
          bumpLevel(1);
          break;
        case "error":
          playTone(190, 96, 0.2, "triangle", 0, gainScale * 0.9);
          playTone(135, 82, 0.18, "sawtooth", 0.04, gainScale * 0.25);
          bumpLevel(0.9);
          break;
      }
    },
    [bumpLevel, ensureContext, sfxEnabled, volume]
  );

  const toggleSfx = useCallback(() => {
    setSfxEnabled((prev) => {
      const next = !prev;
      writeFlag(SFX_KEY, next);
      if (next) {
        const ctx = ensureContext();
        if (ctx) {
          const now = ctx.currentTime;
          const osc = ctx.createOscillator();
          const gain = ctx.createGain();
          osc.type = "sine";
          osc.frequency.setValueAtTime(587.33, now);
          osc.frequency.exponentialRampToValueAtTime(880, now + 0.08);
          gain.gain.setValueAtTime(0.0001, now);
          gain.gain.linearRampToValueAtTime(0.05, now + 0.01);
          gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.1);
          osc.connect(gain);
          gain.connect(ctx.destination);
          osc.start(now);
          osc.stop(now + 0.12);
          bumpLevel(0.8);
        }
      } else {
        stopAmbient();
        setAmbientEnabled(false);
        writeFlag(AMBIENT_KEY, false);
      }
      return next;
    });
  }, [bumpLevel, ensureContext, stopAmbient]);

  const toggleAmbient = useCallback(() => {
    setAmbientEnabled((prev) => {
      const next = !prev;
      writeFlag(AMBIENT_KEY, next);
      if (next) {
        if (!sfxEnabled) {
          setSfxEnabled(true);
          writeFlag(SFX_KEY, true);
        }
        startAmbient(volume);
      } else {
        stopAmbient();
      }
      return next;
    });
  }, [sfxEnabled, startAmbient, stopAmbient, volume]);

  const setVolume = useCallback((v: number) => {
    setVolumeState(Math.min(1, Math.max(0, v)));
  }, []);

  const value = useMemo(
    () => ({
      sfxEnabled,
      ambientEnabled,
      volume,
      level,
      toggleSfx,
      toggleAmbient,
      setVolume,
      play,
    }),
    [ambientEnabled, level, play, setVolume, sfxEnabled, toggleAmbient, toggleSfx, volume]
  );

  return <AudioEngineContext.Provider value={value}>{children}</AudioEngineContext.Provider>;
}

export function useAudioEngine(): AudioEngineContextValue {
  const ctx = useContext(AudioEngineContext);
  if (!ctx) {
    return {
      sfxEnabled: false,
      ambientEnabled: false,
      volume: 0.45,
      level: 0,
      toggleSfx: () => undefined,
      toggleAmbient: () => undefined,
      setVolume: () => undefined,
      play: () => undefined,
    };
  }
  return ctx;
}

export function SoundToggle({ compact = false }: { compact?: boolean }) {
  const { sfxEnabled, ambientEnabled, level, toggleSfx, toggleAmbient } = useAudioEngine();

  return (
    <div className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] p-0.5 shrink-0">
      <button
        type="button"
        onClick={toggleSfx}
        aria-pressed={sfxEnabled}
        aria-label={sfxEnabled ? "Mute interactive sound effects" : "Enable interactive sound effects"}
        title={sfxEnabled ? "Synth audio on (click to mute)" : "Enable procedural synth audio"}
        className={`inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
          sfxEnabled
            ? "bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--shadow-subtle)]"
            : "text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
        }`}
      >
        {sfxEnabled ? (
          <Volume2 className="w-3.5 h-3.5 text-[var(--action-accent-bg)] shrink-0" aria-hidden="true" />
        ) : (
          <VolumeX className="w-3.5 h-3.5 shrink-0" aria-hidden="true" />
        )}
        <span
          className="inline-flex items-end gap-[2px] h-3 w-3.5"
          aria-hidden="true"
        >
          {[0.55, 1, 0.72, 0.4].map((scale, i) => (
            <span
              key={i}
              className={`w-[2px] rounded-full transition-all duration-150 ${
                sfxEnabled
                  ? level > 0 || ambientEnabled
                    ? "bg-[var(--action-accent-bg)] animate-pulse"
                    : "bg-[var(--status-success-fg)]"
                  : "bg-[var(--text-tertiary)]/50"
              }`}
              style={{
                height: sfxEnabled
                  ? `${Math.round((ambientEnabled || level > 0 ? Math.max(0.45, level) : 0.45) * scale * 11)}px`
                  : "3px",
              }}
            />
          ))}
        </span>
        {!compact && <span className="hidden xl:inline">{sfxEnabled ? "SFX" : "Audio"}</span>}
      </button>

      {sfxEnabled && (
        <button
          type="button"
          onClick={toggleAmbient}
          aria-pressed={ambientEnabled}
          aria-label={ambientEnabled ? "Stop ambient soundscape" : "Play ambient synth drone"}
          title={ambientEnabled ? "Ambient drone playing" : "Play subtle ambient soundscape"}
          className={`inline-flex items-center justify-center rounded-md px-1.5 py-1 text-[11px] font-mono transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] ${
            ambientEnabled
              ? "bg-[var(--status-info-bg)] text-[var(--status-info-fg)] font-semibold"
              : "text-[var(--text-tertiary)] hover:text-[var(--text-primary)]"
          }`}
        >
          <Waves className="w-3 h-3" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
