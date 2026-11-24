import React from "react";
import { Sun, Moon, Monitor } from "lucide-react";
import { useTheme, type Theme } from "./ThemeProvider";

export function ThemeToggle({ className = "" }: { className?: string }) {
  const { theme, setTheme } = useTheme();

  const options: { value: Theme; label: string; icon: React.ReactNode }[] = [
    { value: "light", label: "Light mode", icon: <Sun className="w-3.5 h-3.5" /> },
    { value: "dark", label: "Dark mode", icon: <Moon className="w-3.5 h-3.5" /> },
    { value: "system", label: "System sync", icon: <Monitor className="w-3.5 h-3.5" /> },
  ];

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
      className={`inline-flex items-center p-0.5 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)] ${className}`}
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
            className={`flex items-center gap-1.5 px-2 sm:px-2.5 py-1 text-xs font-medium rounded-md transition-all cursor-pointer ${
              isSelected
                ? "bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-sm font-semibold"
                : "text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
            }`}
          >
            {opt.icon}
            <span className="hidden sm:inline capitalize">{opt.value}</span>
          </button>
        );
      })}
    </div>
  );
}
