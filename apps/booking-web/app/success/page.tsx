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
  if (error) return <main className="page centered"><section className="unavailable"><p className="eyebrow">Randevu kullanılamıyor</p><h1>Randevu bulunamadı</h1><p>Bu randevu alınamadı. Yardıma ihtiyacınız varsa lütfen işletmeyle iletişime geçin.</p><Link className="text-link" href="/">Randevuya dön</Link></section></main>;
  if (!data) return <main className="page centered" aria-live="polite"><div className="spinner" aria-hidden="true" /><p>Randevunuz yükleniyor…</p></main>;
  const { appointment, config } = data;
  return <main className="page centered"><section className="success-card"><p className="success-mark" aria-hidden="true">✓</p><p className="eyebrow">Randevu onaylandı</p><h1>Randevunuz hazır.</h1><p className="success-lede">Onay e-postası {appointment.customer_contact} adresine gönderilecek.</p><dl><div><dt>Tarih ve saat</dt><dd>{displaySlot(appointment.start_at, config.business_timezone)}</dd></div><div><dt>Hizmet</dt><dd>{data.service?.name ?? "Seçilen hizmet"}</dd></div><div><dt>Berber</dt><dd>{data.barber?.display_name ?? "Seçilen berber"}</dd></div><div><dt>Şube</dt><dd>{data.branch?.name ?? "Seçilen şube"}</dd></div><div><dt>Müşteri</dt><dd>{appointment.customer_name}</dd></div></dl><Link className="text-link" href="/">Başka bir randevu al</Link></section></main>;
}
