import React, { useEffect, useMemo, useRef, useState } from "react";
import { Command, ArrowRight, Sun, Moon, Volume2, Layers, Compass, Sparkles, X } from "lucide-react";
import { useTheme } from "../theme/ThemeProvider";
import { useAudioEngine } from "./AudioEngine";
import { emitCelebrationBurst } from "./MotionPrimitives";
import { toast } from "sonner";

interface CommandItem {
  id: string;
  title: string;
  subtitle: string;
  group: "Navigate" | "Demos" | "Controls";
  action: () => void;
  icon: React.ReactNode;
}

const KONAMI = [
  "ArrowUp",
  "ArrowUp",
  "ArrowDown",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "ArrowLeft",
  "ArrowRight",
  "b",
  "a",
];

export function CommandPalette() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);
  const konamiPos = useRef(0);
  const { resolvedTheme, setTheme } = useTheme();
  const { sfxEnabled, toggleSfx, play } = useAudioEngine();

  useEffect(() => {
    const onOpenRequest = () => setOpen(true);

    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen((prev) => {
          const next = !prev;
          if (next) play("modal");
          return next;
        });
        return;
      }

      if (open && e.key === "Escape") {
        e.preventDefault();
        setOpen(false);
        return;
      }

      // Safe Konami Easter egg (does not fire while typing inside inputs)
      const tag = (e.target as HTMLElement | null)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      const expected = KONAMI[konamiPos.current];
      if (e.key === expected || e.key.toLowerCase() === expected) {
        konamiPos.current++;
        if (konamiPos.current === KONAMI.length) {
          konamiPos.current = 0;
          emitCelebrationBurst();
          play("success");
          toast.success("Protocol Overdrive · SQL SUM(debit - credit) = 0", {
            description: "All simulated participant invariants verified cleanly.",
          });
        }
      } else {
        konamiPos.current = e.key === KONAMI[0] ? 1 : 0;
      }
    };

    window.addEventListener("transactx:open-palette", onOpenRequest);
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("transactx:open-palette", onOpenRequest);
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [open, play]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setActiveIndex(0);
      window.setTimeout(() => inputRef.current?.focus(), 20);
    }
  }, [open]);

  const navigate = (path: string) => {
    setOpen(false);
    play("tab");
    window.location.assign(path);
  };

  const items: CommandItem[] = useMemo(
    () => [
      {
        id: "home",
        title: "Home & Architecture Landing",
        subtitle: "/",
        group: "Navigate",
        icon: <Compass className="w-4 h-4" aria-hidden="true" />,
        action: () => navigate("/"),
      },
      {
        id: "showcase",
        title: "Interactive Scenarios & Playground",
        subtitle: "/showcase — 192 scenarios + motion/audio lab",
        group: "Navigate",
        icon: <Sparkles className="w-4 h-4" aria-hidden="true" />,
        action: () => navigate("/showcase"),
      },
      {
        id: "design-system",
        title: "Design System & Token Showcase",
        subtitle: "/design-system",
        group: "Navigate",
        icon: <Layers className="w-4 h-4" aria-hidden="true" />,
        action: () => navigate("/design-system"),
      },
      {
        id: "demo-customer",
        title: "Demo: Customer (Alice · Bank A)",
        subtitle: "/demo?role=customer",
        group: "Demos",
        icon: <ArrowRight className="w-4 h-4" aria-hidden="true" />,
        action: () => navigate("/demo?role=customer"),
      },
      {
        id: "demo-merchant",
        title: "Demo: Merchant (Store · Bank B)",
        subtitle: "/demo?role=merchant",
        group: "Demos",
        icon: <ArrowRight className="w-4 h-4" aria-hidden="true" />,
        action: () => navigate("/demo?role=merchant"),
      },
      {
        id: "demo-operator",
        title: "Demo: Read-only Operator (OPS_VIEWER)",
        subtitle: "/demo?role=operator",
        group: "Demos",
        icon: <ArrowRight className="w-4 h-4" aria-hidden="true" />,
        action: () => navigate("/demo?role=operator"),
      },
      {
        id: "theme-toggle",
        title: `Switch to ${resolvedTheme === "dark" ? "Light" : "Dark"} Appearance`,
        subtitle: "Updates data-theme across all views",
        group: "Controls",
        icon: resolvedTheme === "dark" ? <Sun className="w-4 h-4" aria-hidden="true" /> : <Moon className="w-4 h-4" aria-hidden="true" />,
        action: () => {
          setTheme(resolvedTheme === "dark" ? "light" : "dark");
          play("tab");
          setOpen(false);
        },
      },
      {
        id: "audio-toggle",
        title: sfxEnabled ? "Mute Interactive Synth Audio" : "Enable Interactive Synth Audio",
        subtitle: "Web Audio procedural feedback (never autoplays)",
        group: "Controls",
        icon: <Volume2 className="w-4 h-4" aria-hidden="true" />,
        action: () => {
          toggleSfx();
          setOpen(false);
        },
      },
      {
        id: "celebrate",
        title: "Trigger Simulated Settlement Wave (Easter Egg)",
        subtitle: "Tip: try ↑ ↑ ↓ ↓ ← → ← → B A anywhere",
        group: "Controls",
        icon: <Sparkles className="w-4 h-4" aria-hidden="true" />,
        action: () => {
          setOpen(false);
          emitCelebrationBurst();
          play("success");
          toast.success("SQL Conservation & Merkle Integrity Verified", {
            description: "Zero duplicate debits · Integer paise arithmetic intact.",
          });
        },
      },
    ],
    [play, resolvedTheme, setTheme, sfxEnabled, toggleSfx]
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items;
    return items.filter(
      (item) =>
        item.title.toLowerCase().includes(q) ||
        item.subtitle.toLowerCase().includes(q) ||
        item.group.toLowerCase().includes(q)
    );
  }, [items, query]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="TransactX command palette"
      className="fixed inset-0 z-50 flex items-start justify-center bg-[#060911]/70 px-4 pt-[12vh] backdrop-blur-md"
      onClick={() => setOpen(false)}
    >
      <div
        className="w-full max-w-lg rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-[var(--shadow-modal)] overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-[var(--border-hairline)] px-4 py-3">
          <Command className="w-4 h-4 text-[var(--text-tertiary)] shrink-0" aria-hidden="true" />
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setActiveIndex(0);
            }}
            onKeyDown={(e) => {
              if (e.key === "ArrowDown") {
                e.preventDefault();
                setActiveIndex((i) => Math.min(filtered.length - 1, i + 1));
              } else if (e.key === "ArrowUp") {
                e.preventDefault();
                setActiveIndex((i) => Math.max(0, i - 1));
              } else if (e.key === "Enter" && filtered[activeIndex]) {
                e.preventDefault();
                filtered[activeIndex].action();
              }
            }}
            placeholder="Jump to route, demo role, or toggle system controls…"
            aria-label="Command search"
            className="w-full bg-transparent text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-none"
          />
          <button
            type="button"
            onClick={() => setOpen(false)}
            aria-label="Close command palette"
            className="rounded-md p-1 text-[var(--text-tertiary)] hover:text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)] cursor-pointer"
          >
            <X className="w-4 h-4" aria-hidden="true" />
          </button>
        </div>

        <ul className="max-h-80 overflow-y-auto p-2 space-y-1">
          {filtered.length === 0 ? (
            <li className="px-3 py-6 text-center text-xs font-mono text-[var(--text-tertiary)]">
              No matching command for "{query}"
            </li>
          ) : (
            filtered.map((item, idx) => {
              const selected = idx === activeIndex;
              return (
                <li key={item.id}>
                  <button
                    type="button"
                    onMouseEnter={() => setActiveIndex(idx)}
                    onClick={item.action}
                    className={`w-full flex items-center justify-between gap-3 rounded-xl px-3 py-2.5 text-left transition-colors cursor-pointer ${
                      selected
                        ? "bg-[var(--status-info-bg)] text-[var(--status-info-fg)]"
                        : "text-[var(--text-primary)] hover:bg-[var(--bg-surface-raised)]"
                    }`}
                  >
                    <span className="flex items-center gap-3 min-w-0">
                      <span className="shrink-0 opacity-80">{item.icon}</span>
                      <span className="min-w-0">
                        <span className="block text-xs font-semibold truncate">{item.title}</span>
                        <span className="block text-[11px] font-mono text-[var(--text-tertiary)] truncate">
                          {item.subtitle}
                        </span>
                      </span>
                    </span>
                    <span className="font-mono text-[10px] uppercase tracking-wider opacity-70 shrink-0">
                      {item.group}
                    </span>
                  </button>
                </li>
              );
            })
          )}
        </ul>

        <div className="flex items-center justify-between border-t border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/60 px-4 py-2 text-[11px] font-mono text-[var(--text-tertiary)]">
          <span>↑↓ Navigate · ↵ Select · Esc Close</span>
          <span>⌘K / Ctrl+K</span>
        </div>
      </div>
    </div>
  );
}

export function openCommandPalette() {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("transactx:open-palette"));
  }
}
