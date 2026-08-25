"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { useCustomerSession } from "@/components/session";

export default function Account() {
  const router = useRouter(); const { state, customer, refresh } = useCustomerSession(); const [busy, setBusy] = useState(false); const [error, setError] = useState("");
  useEffect(() => { if (state === "unauthenticated") router.replace("/login"); }, [router, state]);
  if (state === "loading") return <main className="account-page" aria-live="polite"><p>Hesap yükleniyor…</p></main>;
  if (state === "error") return <main className="account-page"><p className="error" role="alert">Oturumunuz doğrulanamadı. Lütfen tekrar deneyin.</p><button className="book-button" onClick={() => void refresh()}>Tekrar dene</button></main>;
  if (state === "unauthenticated" || !customer) return <main className="account-page" aria-live="polite"><p>Giriş sayfasına yönlendiriliyorsunuz…</p></main>;
  async function logout() { if (busy) return; setBusy(true); setError(""); try { await apiClient.post("/v1/public/customer/auth/logout"); await refresh(); router.push("/"); } catch (cause) { setError(cause instanceof ApiError ? cause.message : "Çıkış yapılamadı. Lütfen tekrar deneyin."); } finally { setBusy(false); } }
  return <main className="account-page"><Link className="eyebrow" href="/">← Randevu al</Link><h1>Merhaba, {customer.name}</h1><p className="account-lede">Randevularınızı ve hesap bilgilerinizi yönetin.</p>{customer.must_change_password && <p className="error" role="alert">Devam etmeden önce şifrenizi değiştirmelisiniz.</p>}<nav className="account-nav" aria-label="Hesap menüsü"><Link href="/account/appointments">Randevularım</Link><Link href="/account/profile">Profil</Link><Link href="/account/security">Güvenlik</Link></nav>{error && <p className="error" role="alert">{error}</p>}<button className="account-secondary" disabled={busy} onClick={() => void logout()}>{busy ? "Çıkış yapılıyor…" : "Çıkış yap"}</button></main>;
}
