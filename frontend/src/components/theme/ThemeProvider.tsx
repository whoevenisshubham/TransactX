import React, { createContext, useContext, useEffect, useState } from "react";

export type Theme = "light" | "dark" | "system";

type ThemeProviderState = {
  theme: Theme;
  resolvedTheme: "light" | "dark";
  setTheme: (theme: Theme) => void;
};

const THEME_STORAGE_KEY = "transactx.theme";
let memoryPreference: Theme | null = null;

function safeReadTheme(key: string, defaultTheme: Theme): Theme {
  if (memoryPreference) return memoryPreference;
  if (typeof window === "undefined") return defaultTheme;
  try {
    const stored = localStorage.getItem(key) as Theme | null;
    if (stored === "light" || stored === "dark" || stored === "system") {
      return stored;
    }
  } catch {}
  return defaultTheme;
}

function safeWriteTheme(key: string, theme: Theme): void {
  try {
    localStorage.setItem(key, theme);
    memoryPreference = null;
  } catch {
    memoryPreference = theme;
  }
}

const ThemeContext = createContext<ThemeProviderState | undefined>(undefined);

export function ThemeProvider({
  children,
  defaultTheme = "system",
  storageKey = THEME_STORAGE_KEY,
}: {
  children: React.ReactNode;
  defaultTheme?: Theme;
  storageKey?: string;
}) {
  const [theme, setThemeState] = useState<Theme>(() => safeReadTheme(storageKey, defaultTheme));

  const [resolvedTheme, setResolvedTheme] = useState<"light" | "dark">(() => {
    if (typeof window !== "undefined") {
      if (theme === "light" || theme === "dark") return theme;
      return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    return "light";
  });

  useEffect(() => {
    const syncTheme = () => {
      const current = safeReadTheme(storageKey, defaultTheme);
      setThemeState(current);
    };
    window.addEventListener("transactx:theme", syncTheme);
    window.addEventListener("storage", syncTheme);
    return () => {
      window.removeEventListener("transactx:theme", syncTheme);
      window.removeEventListener("storage", syncTheme);
    };
  }, [storageKey, defaultTheme]);

  useEffect(() => {
    const root = document.documentElement;

    const applyTheme = () => {
      let activeTheme: "light" | "dark" = "light";
      if (theme === "system") {
        activeTheme = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
      } else {
        activeTheme = theme;
      }

      setResolvedTheme(activeTheme);
      root.setAttribute("data-theme", activeTheme);
      root.style.colorScheme = activeTheme;
      if (activeTheme === "dark") {
        root.classList.add("dark");
      } else {
        root.classList.remove("dark");
      }
    };

    applyTheme();

    if (theme === "system") {
      const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
      const listener = () => applyTheme();
      mediaQuery.addEventListener("change", listener);
      return () => mediaQuery.removeEventListener("change", listener);
    }
  }, [theme]);

  const setTheme = (newTheme: Theme) => {
    safeWriteTheme(storageKey, newTheme);
    setThemeState(newTheme);
    window.dispatchEvent(new Event("transactx:theme"));
  };

  return (
    <ThemeContext.Provider value={{ theme, resolvedTheme, setTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme(): ThemeProviderState {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}
