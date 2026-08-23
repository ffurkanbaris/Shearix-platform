"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { displaySlot } from "@/lib/date";
import type { Appointment, Barber, Branch, CatalogService, TenantConfig } from "@/lib/types";

type Data = { config: TenantConfig; appointment: Appointment; branch?: Branch; barber?: Barber; service?: CatalogService };

export default function SuccessPage() {
  const [data, setData] = useState<Data>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("appointment_id");
    if (!id) { setError(new ApiError(404)); return; }
    void (async () => {
      try {
        const config = await apiClient.get<TenantConfig>("/v1/public/config");
        if (config.app_type !== "booking") throw new ApiError(404);
        const [appointment, branches, barbers, services] = await Promise.all([
          apiClient.get<Appointment>(`/v1/public/appointments/${encodeURIComponent(id)}`),
          apiClient.get<Branch[]>("/v1/public/branches"),
          apiClient.get<Barber[]>("/v1/public/barbers"),
          apiClient.get<CatalogService[]>("/v1/public/services"),
        ]);
        setData({ config, appointment, branch: branches.find((item) => item.id === appointment.branch_id), barber: barbers.find((item) => item.id === appointment.barber_id), service: services.find((item) => item.id === appointment.service_id) });
      } catch (cause) { setError(cause); }
    })();
  }, []);
  if (error) return <main className="page centered"><section className="unavailable"><p className="eyebrow">Booking unavailable</p><h1>Appointment unavailable</h1><p>We could not retrieve this appointment. Please contact the business if you need help.</p><Link className="text-link" href="/">Back to booking</Link></section></main>;
  if (!data) return <main className="page centered"><div className="spinner" /><p>Loading your appointment…</p></main>;
  const { appointment, config } = data;
  return <main className="page centered"><section className="success-card"><p className="success-mark" aria-hidden="true">✓</p><p className="eyebrow">Appointment confirmed</p><h1>Your chair is waiting.</h1><p className="success-lede">A confirmation email will be sent to {appointment.customer_contact}.</p><dl><div><dt>Date &amp; time</dt><dd>{displaySlot(appointment.start_at, config.business_timezone)}</dd></div><div><dt>Service</dt><dd>{data.service?.name ?? "Selected service"}</dd></div><div><dt>Barber</dt><dd>{data.barber?.display_name ?? "Selected barber"}</dd></div><div><dt>Location</dt><dd>{data.branch?.name ?? "Selected branch"}</dd></div><div><dt>Guest</dt><dd>{appointment.customer_name}</dd></div></dl><Link className="text-link" href="/">Book another appointment</Link></section></main>;
}
