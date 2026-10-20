import { FormEvent, useEffect, useState } from "react";
import { api } from "../api";
import { Badge, BrandMark, Button, Field, Icon, InlineError } from "../components";
import { PublicHeader } from "./LandingPage";
import { authClient, type AuthOptions } from "./authClient";
import { beginGoogleSignIn } from "./googleAuth";

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
  const [googleBusy,setGoogleBusy] = useState(false);
  const [googleError,setGoogleError] = useState("");
  async function googleSignIn() {
    if(googleBusy)return;
    setGoogleBusy(true);setGoogleError("");
    try { await beginGoogleSignIn(); }
    catch { setGoogleBusy(false);setGoogleError("Secure sign-in could not start. Allow this tab's session storage or use a shared demo."); }
  }
  useEffect(() => {
    const controller = new AbortController();
    authClient.options(controller.signal).then(setOptions).catch((failure) => {
      if (!controller.signal.aborted) setOptionsError(failure instanceof Error ? failure.message : "Sign-in options are unavailable.");
    });
    return () => controller.abort();
  }, []);
  const registering = mode === "register" && options?.registrationEnabled === true;
  return <div className="public-site reset-public reset-access"><PublicHeader /><main className="auth-layout experience-auth">
    <section className="auth-intro">
      <div className="auth-intro-copy"><p className="eyebrow">Clear at every step</p><h2>Your payments.<br /><span>The complete picture.</span></h2><p>See what happened, what is known, and what needs another look.</p></div>
      <div className="auth-evidence-note"><span className="auth-note-icon" aria-hidden="true">↳</span><p><strong>Built around evidence.</strong> A bank timeout is never treated as proof of failure. Every outcome stays tied to the original payment.</p></div>
      <div className="intro-foot"><span className="network-pulse" /><span>Simulated payment network · No real money</span></div>
    </section>
    <section className="auth-panel" aria-label="Account access">
      <div className="auth-form-wrap">
        <div className="auth-form-context"><Badge tone="neutral">Private account access</Badge><a href="/demo">Try shared demos <Icon name="arrow" /></a></div>
        {options?.registrationEnabled && <div className="auth-tabs" role="tablist" aria-label="Sign in or register"><button type="button" role="tab" aria-selected={!registering} className={!registering ? "is-active" : ""} onClick={() => onModeChange("login")}>Sign in</button><button type="button" role="tab" aria-selected={registering} className={registering ? "is-active" : ""} onClick={() => onModeChange("register")}>Create account</button></div>}
        {registering ? <RegisterForm onAuthenticated={onAuthenticated} /> : <LoginForm onAuthenticated={onAuthenticated} />}
        <div className="auth-provider"><span>or use a provider</span>{options?.googleEnabled ? <><Button variant="secondary" onClick={()=>void googleSignIn()} disabled={googleBusy}>{googleBusy?"Opening Google…":"Continue with Google"}</Button><p>Private customer sign-in. Shared merchant and operator demos are separate.</p></> : <p>Google sign-in is not configured for this build. Password sign-in and shared demos remain available.</p>}{googleError&&<InlineError message={googleError}/>}</div>
        {optionsError && <p className="auth-options-error" role="status">{optionsError} Password sign-in can still be attempted.</p>}
        <p className="auth-legal">Use synthetic information only. Shared demo activity is visible to other visitors. <a href="/privacy">Privacy & demo data</a></p>
      </div>
    </section>
  </main></div>;
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
    <form className="auth-form" onSubmit={submit}>
      <div className="form-heading">
        <p className="eyebrow">Welcome back</p>
        <h1>Sign in to continue</h1>
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
        type={showPassword ? "text" : "password"}
        autoComplete="current-password"
        required
      />
      <button type="button" className="password-visibility" aria-pressed={showPassword} onClick={() => setShowPassword(value => !value)}>{showPassword ? "Hide password" : "Show password"}</button>
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
        <h1>Create your account</h1>
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

