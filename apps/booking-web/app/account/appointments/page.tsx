"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ApiError, apiClient } from "@/lib/api";
import { displaySlot } from "@/lib/date";
import type { Appointment, Branch, Barber, CatalogService, TenantConfig } from "@/lib/types";

type Lookup = { config: TenantConfig; branches: Branch[]; barbers: Barber[]; services: CatalogService[] };

export default function Appointments() {
  const router = useRouter();
  const [upcoming, setUpcoming] = useState<Appointment[]>([]);
  const [history, setHistory] = useState<Appointment[]>([]);
  const [lookup, setLookup] = useState<Lookup>();
  const [refresh, setRefresh] = useState(0);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    void Promise.all([
      apiClient.get<Appointment[]>("/v1/public/customer/appointments/upcoming"),
      apiClient.get<Appointment[]>("/v1/public/customer/appointments/history"),
      apiClient.get<TenantConfig>("/v1/public/config"),
      apiClient.get<Branch[]>("/v1/public/branches"),
      apiClient.get<Barber[]>("/v1/public/barbers"),
      apiClient.get<CatalogService[]>("/v1/public/services"),
    ]).then(([a, h, config, branches, barbers, services]) => {
      setUpcoming(a); setHistory(h); setLookup({ config, branches, barbers, services }); setError(""); setLoading(false);
    }).catch((cause: unknown) => {
      if (cause instanceof ApiError && cause.status === 401) router.replace("/login");
      else { setError("Randevular geçici olarak kullanılamıyor. Lütfen tekrar deneyin."); setLoading(false); }
    });
  }, [router, refresh]);
  return <main className="account-page wide"><Link className="eyebrow" href="/account">← Hesap</Link><h1>Randevular</h1>{loading && <p aria-live="polite">Randevular yükleniyor…</p>}{error && <p className="error" role="alert">{error} <button type="button" onClick={() => { setLoading(true); setRefresh((value) => value + 1); }}>Tekrar dene</button></p>}{lookup && <><AppointmentList title="Yaklaşan" items={upcoming} lookup={lookup} onChanged={() => setRefresh((v) => v + 1)} /><AppointmentList title="Geçmiş" items={history} lookup={lookup} /></>}</main>;
}

function AppointmentList({ title, items, lookup, onChanged }: { title: string; items: Appointment[]; lookup: Lookup; onChanged?: () => void }) {
  const [confirming, setConfirming] = useState<string>();
  const [error, setError] = useState("");
  const [cancelling, setCancelling] = useState(false);
  async function cancel(id: string) {
    setCancelling(true); setError("");
    try { await apiClient.post(`/v1/public/customer/appointments/${id}/cancel`); setConfirming(undefined); onChanged?.(); }
    catch (e) { setError(e instanceof ApiError ? e.message : "Randevu iptal edilemedi."); }
    finally { setCancelling(false); }
  }
  const statuses: Record<Appointment["status"], string> = { pending: "Bekliyor", confirmed: "Onaylandı", cancelled: "İptal edildi", completed: "Tamamlandı", no_show: "Gelmedi" };
  return <section className="account-section"><h2>{title}</h2>{error && <p className="error" role="alert">{error}</p>}{items.length === 0 ? <p className="empty">Henüz burada randevu yok.</p> : items.map((a) => <div className="appointment-row" key={a.id}>
    <div><strong>{displaySlot(a.start_at, lookup.config.business_timezone)}</strong><small>{lookup.services.find((x) => x.id === a.service_id)?.name ?? "Randevu"} · {lookup.barbers.find((x) => x.id === a.barber_id)?.display_name ?? "Berber"} · {lookup.branches.find((x) => x.id === a.branch_id)?.name ?? "Şube"}</small>{a.cancellation_deadline && a.can_cancel && <small>Son iptal: {displaySlot(a.cancellation_deadline, lookup.config.business_timezone)}</small>}</div>
    <span>{statuses[a.status]}</span>
    {title === "Yaklaşan" && a.can_cancel && <button className="account-secondary" type="button" onClick={() => setConfirming(a.id)}>İptal et</button>}
    {title === "Yaklaşan" && a.cancellation_deadline && !a.can_cancel && <small>Çevrimiçi iptal kullanılamıyor</small>}
    {confirming === a.id && <div className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby={`cancel-${a.id}`}><strong id={`cancel-${a.id}`}>Randevu iptal edilsin mi?</strong><p>{displaySlot(a.start_at, lookup.config.business_timezone)} · {lookup.services.find((x) => x.id === a.service_id)?.name ?? "Randevu"}</p><button type="button" autoFocus onClick={() => setConfirming(undefined)} disabled={cancelling}>Randevuyu koru</button><button type="button" onClick={() => void cancel(a.id)} disabled={cancelling}>{cancelling ? "İptal ediliyor…" : "Randevuyu iptal et"}</button></div>}
  </div>)}</section>;
}
