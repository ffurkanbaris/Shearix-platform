"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { apiClient } from "@/lib/api";
import { displayDateTime } from "@/lib/format";
import type { Appointment, Barber, CatalogService, TenantConfig } from "@/lib/types";
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
        <PageHeading eyebrow="Operations" title="Dashboard" />
        <ErrorNotice error={error} onRetry={() => { setError(undefined); load(); }} />
      </>
    );
  }

  if (!data) {
    return (
      <>
        <PageHeading eyebrow="Operations" title="Dashboard" description="Your team's day at a glance." />
        <LoadingState />
      </>
    );
  }

  const now = new Date();
  const tz  = data.config.business_timezone;

  const today = data.appointments
    .filter((a) => new Date(a.start_at).toDateString() === now.toDateString() && a.status !== "cancelled")
    .sort((a, b) => a.start_at.localeCompare(b.start_at));

  const upcoming = data.appointments
    .filter((a) => new Date(a.start_at) >= now && a.status !== "cancelled" && a.status !== "completed")
    .sort((a, b) => a.start_at.localeCompare(b.start_at))
    .slice(0, 8);

  const activeBarbers  = data.barbers.filter((b) => b.active).length;
  const activeServices = data.services.filter((s) => s.active).length;

  const title = data.config.business_name ? `${data.config.business_name}` : "Dashboard";
  const dateLabel = new Intl.DateTimeFormat(undefined, { weekday: "long", month: "long", day: "numeric", timeZone: tz }).format(now);
  const time = (value: string) => new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: tz }).format(new Date(value));

  return (
    <>
      <PageHeading
        eyebrow="Today"
        title={title}
        description={dateLabel}
        action={
          <Link href="/appointments" className="button secondary sm">View all appointments</Link>
        }
      />

      <section aria-label="Today at a glance">
        <div className="today-lead">
          <span className="eyebrow">Appointments scheduled</span>
          <strong className="today-number">{today.length}</strong>
          <span className="muted">{today.length === 1 ? "booking" : "bookings"} on the diary today</span>
        </div>
      </section>

      <div className="kpi-grid" aria-label="Key metrics">
        <div className="kpi">
          <span className="kpi-label">Appointments</span>
          <span className="kpi-value">{today.length}</span>
        </div>
        <div className="kpi">
          <span className="kpi-label">Barbers</span>
          <span className="kpi-value">{activeBarbers}</span>
          <span className="kpi-sub">active roster</span>
        </div>
        <div className="kpi">
          <span className="kpi-label">Services</span>
          <span className="kpi-value">{activeServices}</span>
          <span className="kpi-sub">bookable services</span>
        </div>
        <div className="kpi">
          <span className="kpi-label">Remaining slots</span>
          <span className="kpi-value">—</span>
          <span className="kpi-sub">checked per booking</span>
        </div>
      </div>

      <section className="panel dashboard-timeline-panel">
          <div className="panel-header">
            <div className="panel-header-text">
              <span className="eyebrow">Schedule</span>
              <h2>Today&apos;s timeline</h2>
              <p className="muted">{dateLabel} · {tz}</p>
            </div>
          </div>
          <Timeline
            items={today.map((appointment) => ({
              id: appointment.id,
              time: time(appointment.start_at),
              title: appointment.customer_name,
              detail: `Appointment · ${displayDateTime(appointment.start_at, tz)}`,
              status: appointment.status.replace("_", " "),
            }))}
            empty={<EmptyState title="No appointments today" body="Your next booking will appear here." />}
          />
      </section>

      <section className="panel">
          <div className="panel-header">
            <div className="panel-header-text">
              <span className="eyebrow">Next up</span>
              <h2>Upcoming</h2>
              <p className="muted">The next appointments across the business.</p>
            </div>
          </div>
          {upcoming.length === 0 ? (
            <EmptyState title="No upcoming appointments" body="New appointments will appear here as they are created." />
          ) : (
            <div className="list">
              {upcoming.map((a) => (
                <div className="list-row" key={a.id}>
                  <div className="list-row-main">
                    <strong>{a.customer_name}</strong>
                    <span>{displayDateTime(a.start_at, tz)}</span>
                  </div>
                  <StatusIndicator label={a.status.replace("_", " ")} active={a.status === "confirmed" || a.status === "pending"} />
                </div>
              ))}
            </div>
          )}
      </section>
    </>
  );
}
