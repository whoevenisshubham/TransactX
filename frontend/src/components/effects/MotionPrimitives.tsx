import React, { useEffect, useRef, useState } from "react";
import { motion, useReducedMotion, useScroll, useSpring } from "motion/react";

export function ScrollProgressBar() {
  const { scrollYProgress } = useScroll();
  const scaleX = useSpring(scrollYProgress, {
    stiffness: 220,
    damping: 32,
    restDelta: 0.001,
  });

  return (
    <motion.div
      aria-hidden="true"
      style={{ scaleX, transformOrigin: "0% 50%" }}
      className="fixed top-0 left-0 right-0 z-50 h-[2px] bg-gradient-to-r from-[var(--blueprint-cobalt,#2563eb)] via-[var(--blueprint-ice,#93b4ff)] to-[var(--blueprint-emerald,#10b981)] pointer-events-none"
    />
  );
}

export function Reveal({
  children,
  className = "",
  delay = 0,
  y = 18,
}: {
  children: React.ReactNode;
  className?: string;
  delay?: number;
  y?: number;
}) {
  const reduce = useReducedMotion();
  if (reduce) {
    return <div className={className}>{children}</div>;
  }

  return (
    <motion.div
      initial={{ opacity: 0.98, y, scale: 0.99 }}
      whileInView={{ opacity: 1, y: 0, scale: 1 }}
      viewport={{ once: true, amount: 0.12 }}
      transition={{ duration: 0.44, delay, ease: [0.22, 1, 0.36, 1] }}
      className={className}
    >
      {children}
    </motion.div>
  );
}

export function StaggerGroup({
  children,
  className = "",
  stagger = 0.055,
}: {
  children: React.ReactNode;
  className?: string;
  stagger?: number;
}) {
  const reduce = useReducedMotion();
  if (reduce) {
    return <div className={className}>{children}</div>;
  }

  return (
    <motion.div
      initial="hidden"
      whileInView="visible"
      viewport={{ once: true, amount: 0.12 }}
      variants={{
        hidden: {},
        visible: { transition: { staggerChildren: stagger } },
      }}
      className={className}
    >
      {children}
    </motion.div>
  );
}

export function StaggerItem({
  children,
  className = "",
}: {
  children: React.ReactNode;
  className?: string;
}) {
  const reduce = useReducedMotion();
  if (reduce) {
    return <div className={className}>{children}</div>;
  }

  return (
    <motion.div
      variants={{
        hidden: { opacity: 0.98, y: 12, scale: 0.988 },
        visible: {
          opacity: 1,
          y: 0,
          scale: 1,
          transition: { duration: 0.38, ease: [0.22, 1, 0.36, 1] },
        },
      }}
      className={className}
    >
      {children}
    </motion.div>
  );
}

export function AnimatedCounter({
  value,
  decimals = 0,
  prefix = "",
  suffix = "",
  durationMs = 950,
  className = "",
}: {
  value: number;
  decimals?: number;
  prefix?: string;
  suffix?: string;
  durationMs?: number;
  className?: string;
}) {
  const reduce = useReducedMotion();
  const ref = useRef<HTMLSpanElement | null>(null);
  const [display, setDisplay] = useState<number>(reduce ? value : 0);
  const hasAnimated = useRef(false);

  useEffect(() => {
    if (reduce) {
      setDisplay(value);
      return;
    }

    const node = ref.current;
    if (!node || typeof IntersectionObserver === "undefined") {
      setDisplay(value);
      return;
    }

    let rafId = 0;
    const run = () => {
      const start = performance.now();
      const from = hasAnimated.current ? display : 0;
      hasAnimated.current = true;
      const tick = (now: number) => {
        const progress = Math.min(1, (now - start) / durationMs);
        const eased = 1 - Math.pow(1 - progress, 3);
        setDisplay(from + (value - from) * eased);
        if (progress < 1) rafId = requestAnimationFrame(tick);
      };
      rafId = requestAnimationFrame(tick);
    };

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          observer.disconnect();
          run();
        }
      },
      { threshold: 0.2 }
    );
    observer.observe(node);

    return () => {
      observer.disconnect();
      if (rafId) cancelAnimationFrame(rafId);
    };
  }, [value, reduce, durationMs]);

  const formatted =
    decimals > 0
      ? display.toLocaleString("en-IN", {
          minimumFractionDigits: decimals,
          maximumFractionDigits: decimals,
        })
      : Math.round(display).toLocaleString("en-IN");

  return (
    <span ref={ref} className={`tabular-nums ${className}`}>
      {prefix}
      {formatted}
      {suffix}
    </span>
  );
}

const SCRAMBLE_GLYPHS = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#$%&*+";

export function ScrambleText({
  text,
  className = "",
  scrambleOnHover = true,
}: {
  text: string;
  className?: string;
  scrambleOnHover?: boolean;
}) {
  const reduce = useReducedMotion();
  const [rendered, setRendered] = useState(text);
  const timerRef = useRef<number | null>(null);

  const trigger = () => {
    if (reduce) {
      setRendered(text);
      return;
    }
    if (timerRef.current) window.clearInterval(timerRef.current);
    let step = 0;
    const totalSteps = Math.max(8, Math.min(16, text.length));
    timerRef.current = window.setInterval(() => {
      step++;
      const settled = Math.floor((step / totalSteps) * text.length);
      const next = text
        .split("")
        .map((ch, idx) => {
          if (ch === " " || idx < settled) return ch;
          return SCRAMBLE_GLYPHS[(idx + step * 3) % SCRAMBLE_GLYPHS.length];
        })
        .join("");
      setRendered(next);
      if (step >= totalSteps) {
        if (timerRef.current) window.clearInterval(timerRef.current);
        setRendered(text);
      }
    }, 28);
  };

  useEffect(() => {
    setRendered(text);
    return () => {
      if (timerRef.current) window.clearInterval(timerRef.current);
    };
  }, [text]);

  return (
    <span
      className={className}
      onMouseEnter={scrambleOnHover ? trigger : undefined}
    >
      {rendered}
    </span>
  );
}

export function AnimatedCheckmark({
  size = 20,
  className = "",
}: {
  size?: number;
  className?: string;
}) {
  const reduce = useReducedMotion();
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2.2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      <motion.path
        d="M5 13l4 4L19 7"
        initial={reduce ? { pathLength: 1 } : { pathLength: 0 }}
        animate={{ pathLength: 1 }}
        transition={{ duration: 0.4, ease: "easeOut" }}
      />
    </svg>
  );
}

export type SentinelState = "idle" | "probing" | "success" | "warning" | "error";

const SENTINEL_PATTERNS: Record<SentinelState, number[][]> = {
  idle: [
    [0.35, 0.7, 0.35],
    [0.7, 1, 0.7],
    [0.35, 0.7, 0.35],
  ],
  probing: [
    [1, 0.45, 0.25],
    [0.45, 1, 0.45],
    [0.25, 0.45, 1],
  ],
  success: [
    [0.25, 0.3, 1],
    [1, 0.5, 0.9],
    [0.35, 1, 0.35],
  ],
  warning: [
    [0.3, 1, 0.3],
    [0.3, 1, 0.3],
    [0.2, 0.9, 0.2],
  ],
  error: [
    [1, 0.2, 1],
    [0.2, 1, 0.2],
    [1, 0.2, 1],
  ],
};

export function DotSentinel({
  state = "idle",
  label,
}: {
  state?: SentinelState;
  label?: string;
}) {
  const reduce = useReducedMotion();
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (reduce || state === "idle") return;
    const id = window.setInterval(() => setTick((t) => (t + 1) % 3), 420);
    return () => window.clearInterval(id);
  }, [state, reduce]);

  const grid = SENTINEL_PATTERNS[state];
  const colorVar =
    state === "success"
      ? "var(--status-success-fg)"
      : state === "warning"
      ? "var(--status-warning-fg)"
      : state === "error"
      ? "var(--status-danger-fg)"
      : "var(--action-accent-bg)";

  return (
    <span
      className="inline-grid grid-cols-3 gap-[2.5px] p-1 rounded-md border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] shrink-0"
      aria-label={label ?? `System telemetry state: ${state}`}
      role={label ? "img" : undefined}
      aria-hidden={label ? undefined : true}
    >
      {grid.flatMap((row, r) =>
        row.map((baseOpacity, c) => {
          const active = state === "probing" && (r + c) % 3 === tick;
          return (
            <span
              key={`${r}-${c}`}
              className="w-[3.5px] h-[3.5px] rounded-[1px] transition-opacity duration-200"
              style={{
                backgroundColor: colorVar,
                opacity: active ? 1 : baseOpacity,
              }}
            />
          );
        })
      )}
    </span>
  );
}

export function emitCelebrationBurst(origin?: { x?: number; y?: number }) {
  if (typeof window === "undefined") return;
  if (window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
  window.dispatchEvent(
    new CustomEvent("transactx:celebrate", {
      detail: origin ?? {},
    })
  );
}

export function CelebrationOverlay() {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const activeRafRef = useRef<number>(0);

  useEffect(() => {
    const onCelebrate = (event: Event) => {
      if (activeRafRef.current) {
        cancelAnimationFrame(activeRafRef.current);
        activeRafRef.current = 0;
      }
      const detail = (event as CustomEvent<{ x?: number; y?: number }>).detail ?? {};
      const canvas = canvasRef.current;
      if (!canvas) return;
      const ctx = canvas.getContext("2d");
      if (!ctx) return;

      const dpr = Math.min(2, window.devicePixelRatio || 1);
      const width = window.innerWidth;
      const height = window.innerHeight;
      canvas.width = width * dpr;
      canvas.height = height * dpr;
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

      const cx = detail.x ?? width * 0.5;
      const cy = detail.y ?? height * 0.38;
      const colors = ["#3b82f6", "#10b981", "#93b4ff", "#f59e0b", "#8b5cf6"];
      const particles = Array.from({ length: 42 }, (_, idx) => {
        const angle = (Math.PI * 2 * idx) / 42 + (Math.random() - 0.5) * 0.3;
        const speed = 2.6 + Math.random() * 5.2;
        return {
          x: cx,
          y: cy,
          vx: Math.cos(angle) * speed,
          vy: Math.sin(angle) * speed - 1.8,
          size: 2.2 + Math.random() * 2.8,
          color: colors[idx % colors.length],
          alpha: 1,
        };
      });

      let frame = 0;
      const draw = () => {
        frame++;
        ctx.clearRect(0, 0, width, height);
        for (const p of particles) {
          p.x += p.vx;
          p.y += p.vy;
          p.vy += 0.14;
          p.vx *= 0.985;
          p.alpha = Math.max(0, p.alpha - 0.018);
          ctx.globalAlpha = p.alpha;
          ctx.fillStyle = p.color;
          ctx.beginPath();
          ctx.arc(p.x, p.y, p.size, 0, Math.PI * 2);
          ctx.fill();
        }
        ctx.globalAlpha = 1;
        if (frame < 56) {
          activeRafRef.current = requestAnimationFrame(draw);
        } else {
          activeRafRef.current = 0;
          ctx.clearRect(0, 0, width, height);
        }
      };
      activeRafRef.current = requestAnimationFrame(draw);
    };

    window.addEventListener("transactx:celebrate", onCelebrate);
    return () => {
      window.removeEventListener("transactx:celebrate", onCelebrate);
      if (activeRafRef.current) {
        cancelAnimationFrame(activeRafRef.current);
        activeRafRef.current = 0;
      }
    };
  }, []);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      className="fixed inset-0 z-50 pointer-events-none"
    />
  );
}
