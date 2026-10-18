import { FormEvent, useState } from "react";
import { api } from "../api";
import { Badge, BrandMark, Button, Field, Icon, InlineError } from "../components";
import { ThemeSelect } from "../theme";

export function AuthPage({
  mode,
  onModeChange,
  onAuthenticated,
}: {
  mode: "login" | "register";
  onModeChange: (mode: "login" | "register") => void;
  onAuthenticated: (token: string) => void;
}) {
  return (
    <main className="auth-layout">
      <section className="auth-intro">
        <div className="brand-lockup">
          <BrandMark />
          <span>TransactX</span>
        </div>
        <div className="auth-intro-copy">
          <p className="eyebrow">A calmer way to move money</p>
          <h1>Payments with a clear line of sight.</h1>
          <p>Send money, follow every confirmation, and keep your financial activity precise.</p>
        </div>
        <div className="intro-foot">
          <span className="network-pulse" />
          <span>Secure account access</span>
          <span className="intro-separator">•</span>
          <span>INR payments</span>
          <span className="intro-separator">•</span>
          <span>Simulated payment network</span>
        </div>
      </section>
      <section className="auth-panel">
        <div className="auth-panel-top">
          <ThemeSelect />
          <span className="mobile-brand">
            <BrandMark />
            TransactX
          </span>
          <span className="auth-context">
            <Badge tone="neutral">Customer account</Badge>
          </span>
        </div>
        <div className="auth-form-wrap">
          <div className="auth-tabs" role="tablist" aria-label="Sign in or register">
            <button
              type="button"
              className={mode === "login" ? "is-active" : ""}
              onClick={() => onModeChange("login")}
              role="tab"
              aria-selected={mode === "login"}
            >
              Sign in
            </button>
            <button
              type="button"
              className={mode === "register" ? "is-active" : ""}
              onClick={() => onModeChange("register")}
              role="tab"
              aria-selected={mode === "register"}
            >
              Create account
            </button>
          </div>
          {mode === "login" ? (
            <LoginForm onAuthenticated={onAuthenticated} />
          ) : (
            <RegisterForm onAuthenticated={onAuthenticated} />
          )}
        </div>
        <p className="auth-legal">
          By continuing, you agree to use TransactX as a simulated payment network.
        </p>
      </section>
    </main>
  );
}

function LoginForm({ onAuthenticated }: { onAuthenticated: (token: string) => void }) {
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
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
    <form className="auth-form" onSubmit={submit}>
      <div className="form-heading">
        <p className="eyebrow">Welcome back</p>
        <h2>Sign in to continue</h2>
        <p>Access your balance and payment activity.</p>
      </div>
      <Field
        label="Phone or payment ID"
        value={identifier}
        onChange={setIdentifier}
        placeholder="you@transactx"
        autoComplete="username"
        required
      />
      <Field
        label="Password"
        value={password}
        onChange={setPassword}
        type="password"
        autoComplete="current-password"
        required
      />
      {error && <InlineError message={error} />}
      <Button type="submit" disabled={busy || !identifier.trim() || !password}>
        {busy ? "Signing in…" : "Sign in"}
        <Icon name="arrow" />
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
      await api.register(form);
      const result = await api.login(form.paymentIdentifier.trim(), form.password);
      onAuthenticated(result.token);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "We couldn't create your account.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="auth-form auth-form-register" onSubmit={submit}>
      <div className="form-heading">
        <p className="eyebrow">Start with TransactX</p>
        <h2>Create your account</h2>
        <p>A simple account for clear, deliberate payments.</p>
      </div>
      <div className="form-grid">
        <Field
          label="Full name"
          value={form.name}
          onChange={(value) => update("name", value)}
          autoComplete="name"
          placeholder="Aarav Sharma"
          required
        />
        <Field
          label="Phone"
          value={form.phone}
          onChange={(value) => update("phone", value)}
          autoComplete="tel"
          placeholder="+91 98765 43210"
          required
        />
        <Field
          className="form-grid-wide"
          label="Payment ID"
          value={form.paymentIdentifier}
          onChange={(value) => update("paymentIdentifier", value)}
          placeholder="aarav@transactx"
          autoComplete="username"
          hint="Your unique address on TransactX."
          required
        />
        <Field
          className="form-grid-wide"
          label="Password"
          value={form.password}
          onChange={(value) => update("password", value)}
          type="password"
          autoComplete="new-password"
          minLength={8}
          hint="Must be at least 8 characters."
          required
        />
      </div>
      {error && <InlineError message={error} />}
      <Button
        type="submit"
        disabled={
          busy ||
          !form.name.trim() ||
          !form.phone.trim() ||
          !form.paymentIdentifier.trim() ||
          form.password.length < 8
        }
      >
        {busy ? "Creating account…" : "Create account"}
        <Icon name="arrow" />
      </Button>
    </form>
  );
}

