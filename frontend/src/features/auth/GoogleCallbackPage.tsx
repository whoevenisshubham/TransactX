import React, { useEffect, useState } from "react";
import { completeGoogleSignIn } from "../../public/googleAuth";
import { PublicHeader } from "../public/PublicHeader";
import { Button } from "../../components/ui/button";
import { Card, CardContent } from "../../components/ui/card";
import { AlertCircle, CheckCircle2, Loader2, ArrowRight } from "lucide-react";

export function GoogleCallbackPage({
  onAuthenticated,
}: {
  onAuthenticated: (token: string) => void;
}) {
  const [error, setError] = useState("");
  const [newToken, setNewToken] = useState("");

  useEffect(() => {
    let active = true;
    completeGoogleSignIn()
      .then((result) => {
        if (!active) return;
        if (result.newAccount) {
          setNewToken(result.token);
        } else {
          window.history.replaceState({}, "", "/");
          onAuthenticated(result.token);
        }
      })
      .catch((err) => {
        if (!active) return;
        const msg =
          err instanceof Error && err.message
            ? err.message
            : "Google sign-in could not be completed or was cancelled. Start again, or explore a shared demo.";
        setError(msg);
      });
    return () => {
      active = false;
    };
  }, [onAuthenticated]);

  return (
    <div className="public-site reset-public reset-access min-h-screen flex flex-col bg-[var(--bg-canvas)] text-[var(--text-primary)] antialiased transition-colors">
      <PublicHeader />

      <main className="google-callback public-container flex-1 w-full max-w-xl mx-auto px-4 sm:px-6 py-16 flex items-center justify-center">
        <Card className="w-full shadow-lg">
          <CardContent className="pt-6 space-y-6 text-center">
            <p className="eyebrow text-xs font-mono font-semibold uppercase tracking-wider text-[var(--action-accent-bg)]">
              Private account access
            </p>

            <h1 className="text-2xl sm:text-3xl font-extrabold tracking-tight text-[var(--text-primary)]">
              {newToken
                ? "Your private sign-in is ready."
                : error
                ? "Let’s try another way."
                : "Verifying your sign-in…"}
            </h1>

            {newToken ? (
              <div className="space-y-6">
                <div className="p-4 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 text-xs text-[var(--text-secondary)] space-y-2 text-left">
                  <div className="flex items-center gap-2 font-semibold text-[var(--status-success-fg)]">
                    <CheckCircle2 className="w-4 h-4 shrink-0" />
                    <span>Cryptographic Keypair Initialized</span>
                  </div>
                  <p>
                    This simulated account starts at zero. Explore a funded shared demo to try payments. Your Google email and real name are not used as your public identity.
                  </p>
                </div>

                <div className="flex flex-col sm:flex-row items-center justify-center gap-3">
                  <Button
                    variant="primary"
                    className="w-full sm:w-auto"
                    onClick={() => {
                      window.history.replaceState({}, "", "/");
                      onAuthenticated(newToken);
                    }}
                  >
                    <span>Open my private account</span>
                    <ArrowRight className="w-3.5 h-3.5 ml-1" />
                  </Button>
                  <a
                    className="button button-secondary w-full sm:w-auto inline-flex items-center justify-center px-4 py-2 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] text-xs font-semibold hover:bg-[var(--bg-surface-raised)] transition-colors shadow-sm"
                    href="/demo"
                  >
                    Explore funded shared demos
                  </a>
                </div>
              </div>
            ) : error ? (
              <div className="space-y-6">
                <div
                  role="alert"
                  className="p-4 rounded-xl border border-[var(--status-danger-border)] bg-[var(--status-danger-bg)] text-xs text-[var(--status-danger-fg)] text-left flex items-start gap-2.5"
                >
                  <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
                  <span>{error}</span>
                </div>

                <div className="flex flex-col sm:flex-row items-center justify-center gap-3">
                  <a
                    className="button button-primary w-full sm:w-auto inline-flex items-center justify-center px-4 py-2 rounded-lg bg-[var(--action-primary-bg)] text-[var(--action-primary-fg)] text-xs font-semibold hover:opacity-90 transition-opacity shadow-sm"
                    href="/login"
                  >
                    Try sign-in again
                  </a>
                  <a
                    className="button button-secondary w-full sm:w-auto inline-flex items-center justify-center px-4 py-2 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface)] text-xs font-semibold hover:bg-[var(--bg-surface-raised)] transition-colors shadow-sm"
                    href="/demo"
                  >
                    Explore shared demos
                  </a>
                </div>
              </div>
            ) : (
              <div className="py-6 space-y-4">
                <Loader2 className="w-8 h-8 animate-spin mx-auto text-[var(--action-accent-bg)]" />
                <p role="status" className="text-xs font-mono text-[var(--text-secondary)]">
                  Exchanging a one-time code securely. No Google tokens are stored in your browser.
                </p>
              </div>
            )}
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
