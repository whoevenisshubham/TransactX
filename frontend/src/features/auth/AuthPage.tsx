import React, { FormEvent, useEffect, useRef, useState } from "react";
import { api } from "../../api";
import { authClient, type AuthOptions } from "../../public/authClient";
import { beginGoogleSignIn } from "../../public/googleAuth";
import { PublicHeader } from "../public/PublicHeader";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Label } from "../../components/ui/label";
import { Card, CardContent } from "../../components/ui/card";
import { Eye, EyeOff, ArrowRight, ShieldCheck, Lock, AlertCircle, Sparkles, CheckCircle2 } from "lucide-react";

export function AuthPage({
  mode,
  onModeChange,
  onAuthenticated,
}: {
  mode: "login" | "register";
  onModeChange: (mode: "login" | "register") => void;
  onAuthenticated: (token: string) => void;
}) {
  const [options, setOptions] = useState<AuthOptions | null>(null);
  const [optionsError, setOptionsError] = useState("");
  const [googleBusy, setGoogleBusy] = useState(false);
  const [googleError, setGoogleError] = useState("");

  async function handleGoogleSignIn() {
    if (googleBusy) return;
    setGoogleBusy(true);
    setGoogleError("");
    try {
      await beginGoogleSignIn();
    } catch {
      setGoogleBusy(false);
      setGoogleError("Secure sign-in could not start. Allow this tab's session storage or use a shared demo.");
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    authClient
      .options(controller.signal)
      .then(setOptions)
      .catch((failure) => {
        if (!controller.signal.aborted) {
          setOptionsError(failure instanceof Error ? failure.message : "Sign-in options are unavailable.");
        }
      });
    return () => controller.abort();
  }, []);

  const registering = mode === "register" && options?.registrationEnabled === true;

  return (
    <div className="min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors">
      <PublicHeader />

      <main className="flex-1 w-full max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-8 sm:py-12 flex items-center justify-center">
        <div className="w-full grid grid-cols-1 lg:grid-cols-12 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-[var(--shadow-raised)] overflow-hidden">
          {/* Left Architectural Brand & Security Overview (lg:col-span-5, 45/55 split) */}
          <section className="lg:col-span-5 bg-[#0f172a] text-[#f8fafc] p-6 sm:p-10 flex flex-col justify-between border-b lg:border-b-0 lg:border-r border-[var(--border-hairline)] space-y-8">
            <div className="space-y-6">
              <div className="flex items-center gap-2">
                <span className="w-2 h-2 rounded-full bg-[#34d399]" />
                <span className="font-mono text-xs uppercase font-semibold text-[#34d399] tracking-wider">
                  DEMO PLATFORM · ISOLATED ENVIRONMENT
                </span>
              </div>

              <div className="space-y-2">
                <p className="text-xs font-mono font-semibold uppercase tracking-wider !text-[#60a5fa] m-0">
                  Clear at every step
                </p>
                <h2 className="text-2xl sm:text-3xl font-extrabold tracking-tight !text-white leading-tight m-0">
                  Your payments.<br />
                  <span className="!text-[#94a3b8]">The complete picture.</span>
                </h2>
                <p className="text-xs !text-[#cbd5e1] leading-relaxed pt-1 m-0">
                  See what happened, what is known, and what needs another look. Balances are derived from double-entry ledger entries and verifiable transaction evidence.
                </p>
              </div>

              <div className="p-4 rounded-xl border border-white/10 bg-white/5 text-xs space-y-1">
                <div className="flex items-center gap-1.5 font-semibold !text-white">
                  <span className="!text-[#60a5fa] font-bold">↳</span>
                  <span>Built around evidence.</span>
                </div>
                <p className="!text-[#cbd5e1] leading-relaxed m-0">
                  A bank timeout is never treated as proof of failure. Central outcomes and participant bank observations stay distinct.
                </p>
              </div>

              {/* Architecture Capabilities */}
              <div className="grid grid-cols-3 gap-2 pt-2 text-center font-mono">
                <div className="p-2.5 rounded-lg border border-white/10 bg-white/5">
                  <div className="font-bold text-sm text-white">Integer</div>
                  <div className="text-[10px] text-[#94a3b8] uppercase mt-0.5">Paise</div>
                </div>
                <div className="p-2.5 rounded-lg border border-white/10 bg-white/5">
                  <div className="font-bold text-sm text-white">SQL Sum</div>
                  <div className="text-[10px] text-[#94a3b8] uppercase mt-0.5">Conservation</div>
                </div>
                <div className="p-2.5 rounded-lg border border-white/10 bg-white/5">
                  <div className="font-bold text-sm text-[#34d399]">UUIDv4</div>
                  <div className="text-[10px] text-[#94a3b8] uppercase mt-0.5">Idempotency</div>
                </div>
              </div>
            </div>

            <div className="pt-6 border-t border-white/10 flex items-center justify-between text-[11px] font-mono text-[#94a3b8]">
              <span className="flex items-center gap-1.5">
                <span className="w-2 h-2 rounded-full bg-[#60a5fa]" />
                <span>Simulated payment network</span>
              </span>
              <span>No real money</span>
            </div>
          </section>

          {/* Right Authentication Form Panel (lg:col-span-7) */}
          <section className="lg:col-span-7 p-6 sm:p-10 flex flex-col justify-center space-y-6 bg-[var(--bg-surface)]" aria-label="Account access">
            <div className="space-y-6 max-w-md mx-auto w-full">
              <div className="auth-form-context flex items-center justify-between pb-2 border-b border-[var(--border-hairline)] text-xs">
                <span className="px-2.5 py-0.5 rounded-full font-mono text-[11px] font-semibold bg-[var(--bg-surface-raised)] text-[var(--text-secondary)] border border-[var(--border-hairline)]">
                  Private account access
                </span>
                <a
                  href="/demo"
                  className="font-semibold text-xs text-[var(--action-accent-bg)] hover:underline inline-flex items-center gap-1"
                >
                  <span>Try shared demos</span>
                  <ArrowRight className="w-3 h-3" />
                </a>
              </div>

              {/* Tabs when Registration is enabled */}
              {options?.registrationEnabled && (
                <div
                  className="auth-tabs flex p-1 rounded-xl bg-[var(--bg-surface-raised)] border border-[var(--border-hairline)]"
                  role="tablist"
                  aria-label="Sign in or register"
                >
                  <button
                    type="button"
                    role="tab"
                    aria-selected={!registering}
                    className={`flex-1 py-1.5 px-3 rounded-lg text-xs font-semibold transition-all cursor-pointer ${
                      !registering
                        ? "is-active bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-sm font-bold"
                        : "text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                    }`}
                    onClick={() => onModeChange("login")}
                  >
                    Sign in
                  </button>
                  <button
                    type="button"
                    role="tab"
                    aria-selected={registering}
                    className={`flex-1 py-1.5 px-3 rounded-lg text-xs font-semibold transition-all cursor-pointer ${
                      registering
                        ? "is-active bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-sm font-bold"
                        : "text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
                    }`}
                    onClick={() => onModeChange("register")}
                  >
                    Create account
                  </button>
                </div>
              )}

              {/* Form rendering */}
              {registering ? (
                <RegisterForm onAuthenticated={onAuthenticated} />
              ) : (
                <LoginForm onAuthenticated={onAuthenticated} />
              )}

              {/* Provider Divider & Google SSO */}
              <div className="auth-provider space-y-3 pt-2 border-t border-[var(--border-hairline)] text-center">
                <span className="block text-[11px] font-mono uppercase text-[var(--text-tertiary)] tracking-wider">
                  or use a provider
                </span>
                {options?.googleEnabled ? (
                  <div className="space-y-2">
                    <Button
                      variant="secondary"
                      className="w-full flex items-center justify-center gap-2"
                      onClick={() => void handleGoogleSignIn()}
                      disabled={googleBusy}
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24">
                        <path
                          d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
                          fill="#4285F4"
                        />
                        <path
                          d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                          fill="#34A853"
                        />
                        <path
                          d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.06H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.94l2.85-2.22.81-.63z"
                          fill="#FBBC05"
                        />
                        <path
                          d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.06l3.66 2.84c.87-2.6 3.3-4.52 6.16-4.52z"
                          fill="#EA4335"
                        />
                      </svg>
                      <span>{googleBusy ? "Opening Google…" : "Continue with Google"}</span>
                    </Button>
                    <p className="text-[11px] text-[var(--text-tertiary)]">
                      Private customer sign-in. Shared merchant and operator demos are separate.
                    </p>
                  </div>
                ) : (
                  <p className="text-xs text-[var(--text-secondary)]">
                    Google sign-in is not configured for this build. Password sign-in and shared demos remain available.
                  </p>
                )}

                {googleError && (
                  <div role="alert" className="p-3 rounded-lg bg-[var(--status-danger-bg)] border border-[var(--status-danger-border)] text-xs text-[var(--status-danger-fg)] text-left flex items-start gap-2">
                    <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
                    <span>{googleError}</span>
                  </div>
                )}
              </div>

              {optionsError && (
                <p className="auth-options-error text-xs font-mono text-[var(--status-warning-fg)] p-2.5 rounded bg-[var(--status-warning-bg)] border border-[var(--status-warning-border)]" role="status">
                  {optionsError} Password sign-in can still be attempted.
                </p>
              )}
            </div>

            <p className="auth-legal text-center text-xs text-[var(--text-secondary)] pt-4 border-t border-[var(--border-hairline)] max-w-md mx-auto w-full">
              Use synthetic information only. Shared demo activity is visible to other visitors.{" "}
              <a href="/privacy" className="text-[var(--action-accent-bg)] hover:underline font-medium">
                Privacy & demo data
              </a>
            </p>
          </section>
        </div>
      </main>
    </div>
  );
}

function LoginForm({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!identifier.trim() || !password) return;
    setBusy(true);
    setError("");
    try {
      const result = await api.login(identifier.trim(), password);
      onAuthenticated(result.token);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "We couldn't sign you in.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="auth-form space-y-4" onSubmit={submit}>
      <div className="form-heading space-y-1">
        <p className="eyebrow text-xs font-mono font-semibold uppercase tracking-wider text-[var(--action-accent-bg)]">
          Welcome back
        </p>
        <h1 className="text-2xl font-bold tracking-tight text-[var(--text-primary)]">
          Sign in to continue
        </h1>
        <p className="text-xs text-[var(--text-secondary)]">
          Access your balance and payment activity.
        </p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="login-identifier" required>
          Phone or payment ID
        </Label>
        <Input
          id="login-identifier"
          value={identifier}
          onChange={(e) => setIdentifier(e.target.value)}
          placeholder="you@transactx"
          autoComplete="username"
          required
        />
      </div>

      <div className="space-y-1.5">
        <div className="flex items-center justify-between">
          <Label htmlFor="login-password" required>
            Password
          </Label>
          <button
            type="button"
            className="password-visibility text-xs font-medium text-[var(--action-accent-bg)] hover:underline cursor-pointer"
            aria-pressed={showPassword}
            onClick={() => setShowPassword((v) => !v)}
          >
            {showPassword ? "Hide password" : "Show password"}
          </button>
        </div>
        <Input
          id="login-password"
          aria-label="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          type={showPassword ? "text" : "password"}
          autoComplete="current-password"
          required
        />
      </div>

      {error && (
        <div
          role="alert"
          className="p-3 rounded-lg bg-[var(--status-danger-bg)] border border-[var(--status-danger-border)] text-xs text-[var(--status-danger-fg)] flex items-start gap-2"
        >
          <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
          <span>{error}</span>
        </div>
      )}

      <Button
        type="submit"
        variant="primary"
        className="w-full justify-center gap-2"
        disabled={busy || !identifier.trim() || !password}
        loading={busy}
      >
        <span>{busy ? "Signing in…" : "Sign in"}</span>
        <ArrowRight className="w-3.5 h-3.5" />
      </Button>
    </form>
  );
}

function RegisterForm({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const [form, setForm] = useState({
    name: "",
    phone: "",
    paymentIdentifier: "",
    password: "",
    role: "CUSTOMER",
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const registeredRef = useRef(false);

  function update(field: keyof typeof form, value: string) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!form.name.trim() || !form.phone.trim() || !form.paymentIdentifier.trim()) {
      setError("Please fill in all required fields.");
      return;
    }
    if (form.password.length < 8) {
      setError("Password must be at least 8 characters long.");
      return;
    }
    setBusy(true);
    setError("");
    try {
      if (!registeredRef.current) {
        await api.register(form);
        registeredRef.current = true;
      }
      const result = await api.login(form.paymentIdentifier.trim(), form.password);
      onAuthenticated(result.token);
    } catch (caught) {
      if (registeredRef.current) {
        setError("Account created successfully. Automatic sign-in failed; please click sign in to proceed.");
      } else {
        setError(caught instanceof Error ? caught.message : "We couldn't create your account.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="auth-form auth-form-register space-y-4" onSubmit={submit}>
      <div className="form-heading space-y-1">
        <p className="eyebrow text-xs font-mono font-semibold uppercase tracking-wider text-[var(--action-accent-bg)]">
          Start with TransactX
        </p>
        <h1 className="text-2xl font-bold tracking-tight text-[var(--text-primary)]">
          Create your account
        </h1>
        <p className="text-xs text-[var(--text-secondary)]">
          A simple account for clear, deliberate payments.
        </p>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div className="space-y-1.5">
          <Label htmlFor="reg-name" required>
            Full name
          </Label>
          <Input
            id="reg-name"
            value={form.name}
            onChange={(e) => update("name", e.target.value)}
            autoComplete="name"
            placeholder="Aarav Sharma"
            required
          />
        </div>

        <div className="space-y-1.5">
          <Label htmlFor="reg-phone" required>
            Phone
          </Label>
          <Input
            id="reg-phone"
            value={form.phone}
            onChange={(e) => update("phone", e.target.value)}
            autoComplete="tel"
            placeholder="+91 98765 43210"
            required
          />
        </div>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="reg-payment-id" required>
          Payment ID
        </Label>
        <Input
          id="reg-payment-id"
          value={form.paymentIdentifier}
          onChange={(e) => update("paymentIdentifier", e.target.value)}
          placeholder="aarav@transactx"
          autoComplete="username"
          required
        />
        <p className="text-[11px] text-[var(--text-tertiary)]">Your unique address on TransactX.</p>
      </div>

      <div className="space-y-1.5">
        <Label htmlFor="reg-password" required>
          Password
        </Label>
        <Input
          id="reg-password"
          aria-label="Password"
          value={form.password}
          onChange={(e) => update("password", e.target.value)}
          type="password"
          autoComplete="new-password"
          minLength={8}
          required
        />
        <p className="text-[11px] text-[var(--text-tertiary)]">Must be at least 8 characters.</p>
      </div>

      {error && (
        <div
          role="alert"
          className="p-3 rounded-lg bg-[var(--status-danger-bg)] border border-[var(--status-danger-border)] text-xs text-[var(--status-danger-fg)] flex items-start gap-2"
        >
          <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
          <span>{error}</span>
        </div>
      )}

      <Button
        type="submit"
        variant="primary"
        className="w-full justify-center gap-2"
        disabled={
          busy ||
          !form.name.trim() ||
          !form.phone.trim() ||
          !form.paymentIdentifier.trim() ||
          form.password.length < 8
        }
        loading={busy}
      >
        <span>{busy ? "Creating account…" : "Create account"}</span>
        <ArrowRight className="w-3.5 h-3.5" />
      </Button>
    </form>
  );
}
