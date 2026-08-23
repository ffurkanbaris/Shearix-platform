"use client";

import { FormEvent, Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ApiError, apiClient } from "@/lib/api";
import type { Principal } from "@/lib/types";
import { safeNext } from "@/lib/redirect";
import { EmailLoginRequest, normalizeAuthEmail } from "../../../shared/auth-contract";

function LoginForm() {
  const router = useRouter();
  const search = useSearchParams();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    setLoading(true);
    try {
      const request: EmailLoginRequest = { email: normalizeAuthEmail(email), password };
      const principal = await apiClient.post<Principal>("/v1/admin/auth/login", request);
	  router.replace(principal.must_change_password ? "/settings" : safeNext(search.get("next")));
      router.refresh();
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : "Unable to sign in.");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="login-page">
      <section className="login-card" aria-labelledby="login-title">
        <div className="login-logo" aria-hidden="true">B</div>
        <h1 id="login-title">Sign in to your panel</h1>
        <p className="muted" style={{ marginBottom: "1.5rem", marginTop: ".25rem" }}>
		  Use the email address and password assigned to your staff account.
        </p>
        <form onSubmit={submit} className="form-stack">
          <label>
			Email
            <input
			  id="login-email"
              autoComplete="username"
			  type="email"
			  value={email}
			  onChange={(e) => setEmail(e.target.value)}
			  placeholder="staff@example.com"
              required
            />
          </label>
          <label>
            Password
            <input
              id="login-password"
              autoComplete="current-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </label>
          {error && <p className="form-error" role="alert">{error}</p>}
          <button className="button primary" disabled={loading} type="submit">
            {loading ? "Signing in…" : "Sign in"}
          </button>
        </form>
      </section>
    </main>
  );
}

export default function LoginPage() { return <Suspense fallback={<main className="centered-state" aria-live="polite">Loading sign in…</main>}><LoginForm /></Suspense>; }
