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
  useEffect(() => {
    void Promise.all([
      apiClient.get<Appointment[]>("/v1/public/customer/appointments/upcoming"),
      apiClient.get<Appointment[]>("/v1/public/customer/appointments/history"),
      apiClient.get<TenantConfig>("/v1/public/config"),
      apiClient.get<Branch[]>("/v1/public/branches"),
      apiClient.get<Barber[]>("/v1/public/barbers"),
      apiClient.get<CatalogService[]>("/v1/public/services"),
    ]).then(([a, h, config, branches, barbers, services]) => {
      setUpcoming(a); setHistory(h); setLookup({ config, branches, barbers, services }); setError("");
    }).catch((cause: unknown) => {
      if (cause instanceof ApiError && cause.status === 401) router.replace("/login");
      else setError("Appointments are temporarily unavailable. Please try again.");
    });
  }, [router, refresh]);
  return <main className="account-page wide"><Link className="eyebrow" href="/account">← Account</Link><h1>Appointments</h1>{error && <p className="error" role="alert">{error} <button type="button" onClick={() => setRefresh((value) => value + 1)}>Retry</button></p>}{lookup && <><AppointmentList title="Upcoming" items={upcoming} lookup={lookup} onChanged={() => setRefresh((v) => v + 1)} /><AppointmentList title="History" items={history} lookup={lookup} /></>}</main>;
}

function AppointmentList({ title, items, lookup, onChanged }: { title: string; items: Appointment[]; lookup: Lookup; onChanged?: () => void }) {
  const [confirming, setConfirming] = useState<string>();
  const [error, setError] = useState("");
  const [cancelling, setCancelling] = useState(false);
  async function cancel(id: string) {
    setCancelling(true); setError("");
    try { await apiClient.post(`/v1/public/customer/appointments/${id}/cancel`); setConfirming(undefined); onChanged?.(); }
    catch (e) { setError(e instanceof ApiError ? e.message : "Unable to cancel appointment."); }
    finally { setCancelling(false); }
  }
  return <section className="account-section"><h2>{title}</h2>{error && <p className="error" role="alert">{error}</p>}{items.length === 0 ? <p className="empty">No appointments here yet.</p> : items.map((a) => <div className="appointment-row" key={a.id}>
    <div><strong>{displaySlot(a.start_at, lookup.config.business_timezone)}</strong><small>{lookup.services.find((x) => x.id === a.service_id)?.name ?? "Appointment"} · {lookup.barbers.find((x) => x.id === a.barber_id)?.display_name ?? "Barber"} · {lookup.branches.find((x) => x.id === a.branch_id)?.name ?? "Location"}</small>{a.cancellation_deadline && a.can_cancel && <small>Cancel by {displaySlot(a.cancellation_deadline, lookup.config.business_timezone)}</small>}</div>
    <span>{a.status}</span>
    {title === "Upcoming" && a.can_cancel && <button className="account-secondary" type="button" onClick={() => setConfirming(a.id)}>Cancel</button>}
    {title === "Upcoming" && a.cancellation_deadline && !a.can_cancel && <small>Online cancellation unavailable</small>}
    {confirming === a.id && <div className="confirm-dialog" role="dialog" aria-modal="true"><strong>Cancel appointment?</strong><p>{displaySlot(a.start_at, lookup.config.business_timezone)} · {lookup.services.find((x) => x.id === a.service_id)?.name ?? "Appointment"}</p><button type="button" onClick={() => setConfirming(undefined)} disabled={cancelling}>Keep appointment</button><button type="button" onClick={() => void cancel(a.id)} disabled={cancelling}>{cancelling ? "Cancelling…" : "Cancel appointment"}</button></div>}
  </div>)}</section>;
}
