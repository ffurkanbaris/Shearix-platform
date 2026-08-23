"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { useCustomerSession } from "@/components/session";

export default function Account() {
  const router = useRouter(); const { state, customer, refresh } = useCustomerSession(); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  useEffect(() => { if (state === "unauthenticated") router.replace("/login"); }, [router, state]);
  if (state === "loading") return <main className="account-page" aria-live="polite"><p>Loading account…</p></main>;
  if (state === "error") return <main className="account-page"><p className="error" role="alert">We could not verify your session. Please try again.</p><button className="book-button" onClick={() => void refresh()}>Try again</button></main>;
  if (state === "unauthenticated" || !customer) return <main className="account-page" aria-live="polite"><p>Redirecting to login…</p></main>;
  async function logout() { setBusy(true); setError(""); try { await apiClient.post("/v1/public/customer/auth/logout"); await refresh(); router.push("/"); } catch (cause) { setError(cause instanceof ApiError ? cause.message : "Unable to sign out. Please try again."); } finally { setBusy(false); } }
  return <main className="account-page"><Link className="eyebrow" href="/">← Book an appointment</Link><h1>Hello, {customer.name}</h1><p className="account-lede">Manage your appointments and account details.</p>{customer.must_change_password && <p className="error" role="alert">You must change your password before continuing.</p>}<nav className="account-nav"><Link href="/account/appointments">My appointments</Link><Link href="/account/profile">Profile</Link><Link href="/account/security">Security</Link></nav>{error && <p className="error" role="alert">{error}</p>}<button className="account-secondary" disabled={busy} onClick={() => void logout()}>{busy ? "Signing out…" : "Log out"}</button></main>;
}
