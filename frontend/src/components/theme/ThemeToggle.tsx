import React from "react";
import { Sun, Moon, Monitor } from "lucide-react";
import { useTheme, type Theme } from "./ThemeProvider";

export function ThemeToggle({
  className = "",
  compact = false,
  iconOnly = false,
  fullWidth = false,
}: {
  className?: string;
  compact?: boolean;
  iconOnly?: boolean;
  fullWidth?: boolean;
}) {
  const { theme, resolvedTheme, setTheme } = useTheme();

  const options: { value: Theme; label: string; shortLabel: string; icon: React.ReactNode }[] = [
    { value: "light", label: "Light mode", shortLabel: "Light", icon: <Sun className="w-3.5 h-3.5 shrink-0" aria-hidden="true" /> },
    { value: "dark", label: "Dark mode", shortLabel: "Dark", icon: <Moon className="w-3.5 h-3.5 shrink-0" aria-hidden="true" /> },
    { value: "system", label: "System sync", shortLabel: "System", icon: <Monitor className="w-3.5 h-3.5 shrink-0" aria-hidden="true" /> },
  ];

  // Compact cycle button for slim desktop icon-rail or tight mobile toolbars
  if (iconOnly) {
    const nextTheme: Theme = theme === "light" ? "dark" : theme === "dark" ? "system" : "light";
    const activeIcon =
      theme === "system" ? (
        <Monitor className="w-4 h-4 shrink-0" aria-hidden="true" />
      ) : resolvedTheme === "dark" ? (
        <Moon className="w-4 h-4 shrink-0" aria-hidden="true" />
      ) : (
        <Sun className="w-4 h-4 shrink-0" aria-hidden="true" />
      );
    return (
      <button
        type="button"
        onClick={() => setTheme(nextTheme)}
        aria-label={`Theme: ${theme} (switch to ${nextTheme})`}
        title={`Theme: ${theme} (switch to ${nextTheme})`}
        className={`inline-flex items-center justify-center w-9 h-9 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] text-[var(--text-primary)] hover:border-[var(--border-active)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] transition-colors cursor-pointer shrink-0 ${className}`}
      >
        {activeIcon}
      </button>
    );
  }

  const handleKeyDown = (e: React.KeyboardEvent, index: number) => {
    let nextIndex = index;
    if (e.key === "ArrowRight" || e.key === "ArrowDown") {
      e.preventDefault();
      nextIndex = (index + 1) % options.length;
    } else if (e.key === "ArrowLeft" || e.key === "ArrowUp") {
      e.preventDefault();
      nextIndex = (index - 1 + options.length) % options.length;
    }
    if (nextIndex !== index) {
      setTheme(options[nextIndex].value);
    }
  };

  return (
    <div
      role="radiogroup"
      aria-label="Theme selection"
      className={`inline-flex items-center p-0.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] shrink-0 ${
        fullWidth ? "w-full justify-between" : ""
      } ${className}`}
    >
      {options.map((opt, idx) => {
        const isSelected = theme === opt.value;
        return (
          <button
            key={opt.value}
            role="radio"
            aria-checked={isSelected}
            aria-label={opt.label}
            title={opt.label}
            tabIndex={isSelected ? 0 : -1}
            type="button"
            onKeyDown={(e) => handleKeyDown(e, idx)}
            onClick={() => setTheme(opt.value)}
            className={`inline-flex items-center justify-center min-h-[28px] ${
              fullWidth
                ? "flex-1 gap-1 px-1.5 py-1 text-[11px]"
                : compact
                ? "gap-1.5 px-2 py-1 text-xs"
                : "gap-1.5 px-2 sm:px-2.5 py-1 text-xs"
            } font-medium rounded-md transition-colors whitespace-nowrap focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] cursor-pointer ${
              isSelected
                ? "bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-[var(--shadow-subtle)] font-semibold"
                : "text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
            }`}
          >
            {opt.icon}
            {!compact && <span className="hidden sm:inline">{opt.shortLabel}</span>}
          </button>
        );
      })}
    </div>
  );
}
