"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { apiClient } from "@/lib/api";
import { dateKeyInTimezone, displayDateTime } from "@/lib/format";
import { appointmentStatusLabels, type Appointment, type Barber, type CatalogService, type TenantConfig } from "@/lib/types";
import { EmptyState, ErrorNotice, LoadingState, PageHeading, StatusIndicator, Timeline } from "@/components/ui";

type DashboardData = { appointments: Appointment[]; barbers: Barber[]; services: CatalogService[]; config: TenantConfig };

export default function DashboardPage() {
  const [data, setData] = useState<DashboardData>();
  const [error, setError] = useState<unknown>();

  function load() {
    const controller = new AbortController();
    void Promise.all([
      apiClient.get<Appointment[]>("/v1/admin/appointments", controller.signal),
      apiClient.get<Barber[]>("/v1/admin/barbers", controller.signal),
      apiClient.get<CatalogService[]>("/v1/admin/services", controller.signal),
      apiClient.get<TenantConfig>("/v1/public/config", controller.signal),
    ]).then(([appointments, barbers, services, config]) =>
      setData({ appointments, barbers, services, config })
    ).catch(setError);
    return controller;
  }

  useEffect(() => {
    const c = load();
    return () => c.abort();
  }, []);

  if (error) {
    return (
      <>
        <PageHeading eyebrow="Operasyon" title="Genel Bakış" />
        <ErrorNotice error={error} onRetry={() => { setError(undefined); load(); }} />
      </>
    );
  }

  if (!data) {
    return (
      <>
        <PageHeading eyebrow="Operasyon" title="Genel Bakış" description="Ekibinizin gününü bir bakışta görün." />
        <LoadingState />
      </>
    );
  }

  const now = new Date();
  const tz  = data.config.business_timezone;

  const todayKey = dateKeyInTimezone(now, tz);
  const today = data.appointments
    .filter((a) => dateKeyInTimezone(a.start_at, tz) === todayKey && a.status !== "cancelled")
    .sort((a, b) => a.start_at.localeCompare(b.start_at));

  const upcoming = data.appointments
    .filter((a) => new Date(a.start_at) >= now && a.status !== "cancelled" && a.status !== "completed")
    .sort((a, b) => a.start_at.localeCompare(b.start_at))
    .slice(0, 8);

  const activeBarbers  = data.barbers.filter((b) => b.active).length;
  const activeServices = data.services.filter((s) => s.active).length;

  const title = data.config.business_name ? `${data.config.business_name}` : "Genel Bakış";
  const dateLabel = new Intl.DateTimeFormat("tr-TR", { weekday: "long", month: "long", day: "numeric", timeZone: tz }).format(now);
  const time = (value: string) => new Intl.DateTimeFormat("tr-TR", { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: tz }).format(new Date(value));

  return (
    <>
      <PageHeading
        eyebrow="Bugün"
        title={title}
        description={dateLabel}
        action={
          <Link href="/appointments" className="button secondary sm">Tüm randevuları görüntüle</Link>
        }
      />

      <section aria-label="Bugünün özeti">
        <div className="today-lead">
          <span className="eyebrow">Planlanan randevular</span>
          <strong className="today-number">{today.length}</strong>
          <span className="muted">bugünkü takvimde {today.length} randevu</span>
        </div>
      </section>

      <div className="kpi-grid" aria-label="Temel göstergeler">
        <div className="kpi">
          <span className="kpi-label">Randevular</span>
          <span className="kpi-value">{today.length}</span>
        </div>
        <div className="kpi">
          <span className="kpi-label">Berberler</span>
          <span className="kpi-value">{activeBarbers}</span>
          <span className="kpi-sub">aktif ekip</span>
        </div>
        <div className="kpi">
          <span className="kpi-label">Hizmetler</span>
          <span className="kpi-value">{activeServices}</span>
          <span className="kpi-sub">randevu alınabilen hizmetler</span>
        </div>
        <div className="kpi">
          <span className="kpi-label">Kalan saatler</span>
          <span className="kpi-value">—</span>
          <span className="kpi-sub">randevu sırasında kontrol edilir</span>
        </div>
      </div>

      <section className="panel dashboard-timeline-panel">
          <div className="panel-header">
            <div className="panel-header-text">
              <span className="eyebrow">Takvim</span>
              <h2>Bugünün akışı</h2>
              <p className="muted">{dateLabel} · {tz}</p>
            </div>
          </div>
          <Timeline
            items={today.map((appointment) => ({
              id: appointment.id,
              time: time(appointment.start_at),
              title: appointment.customer_name,
              detail: `Randevu · ${displayDateTime(appointment.start_at, tz)}`,
              status: appointmentStatusLabels[appointment.status],
            }))}
            empty={<EmptyState title="Bugün randevu yok" body="Bir sonraki randevunuz burada görünecek." />}
          />
      </section>

      <section className="panel">
          <div className="panel-header">
            <div className="panel-header-text">
              <span className="eyebrow">Sırada</span>
              <h2>Yaklaşan randevular</h2>
              <p className="muted">İşletmedeki sıradaki randevular.</p>
            </div>
          </div>
          {upcoming.length === 0 ? (
            <EmptyState title="Yaklaşan randevu yok" body="Yeni randevular oluşturuldukça burada görünecek." />
          ) : (
            <div className="list">
              {upcoming.map((a) => (
                <div className="list-row" key={a.id}>
                  <div className="list-row-main">
                    <strong>{a.customer_name}</strong>
                    <span>{displayDateTime(a.start_at, tz)}</span>
                  </div>
                  <StatusIndicator label={appointmentStatusLabels[a.status]} active={a.status === "confirmed" || a.status === "pending"} />
                </div>
              ))}
            </div>
          )}
      </section>
    </>
  );
}
