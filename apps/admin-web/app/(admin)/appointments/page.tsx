"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { dateKeyInTimezone, displayDateTime, localDate } from "@/lib/format";
import type { Appointment, AvailabilitySlot, Barber, Branch, CatalogService, TenantConfig } from "@/lib/types";
import { appointmentStatusLabels, canWrite } from "@/lib/types";
import { useSession } from "@/components/auth-gate";
import {
  ActionMenu,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorNotice,
  FormError,
  LoadingState,
  Modal,
  PageHeader,
  StatusBadge,
} from "@/components/ui";

type FormState = { branch_id: string; barber_id: string; service_id: string; customer_name: string; customer_contact: string; date: string; start_at: string };
const emptyForm = (): FormState => ({ branch_id: "", barber_id: "", service_id: "", customer_name: "", customer_contact: "", date: localDate(), start_at: "" });

function monday(date: Date): Date {
  const r = new Date(date); const d = r.getDay(); r.setDate(r.getDate() - ((d + 6) % 7)); return r;
}
function isoDate(d: Date): string { return d.toISOString().slice(0, 10); }

export default function AppointmentsPage() {
  const { principal } = useSession();
  const editable = canWrite(principal.role) || principal.role === "RECEPTIONIST";
  const [items, setItems] = useState<Appointment[]>();
  const [barbers, setBarbers] = useState<Barber[]>([]);
  const [branches, setBranches] = useState<Branch[]>([]);
  const [services, setServices] = useState<CatalogService[]>([]);
  const [config, setConfig] = useState<TenantConfig>();
  const [form, setForm] = useState<FormState>(emptyForm);
  const [slots, setSlots] = useState<AvailabilitySlot[]>([]);
  const [editing, setEditing] = useState<Appointment>();
  const [showCreate, setShowCreate] = useState(false);
  const [view, setView] = useState<"day" | "week">("day");
  const [calendarDate, setCalendarDate] = useState(localDate());
  const [error, setError] = useState<unknown>();
  const [formError, setFormError] = useState("");
  const [cancelTarget, setCancelTarget] = useState<Appointment>();
  const [saving, setSaving] = useState(false);
  const [slotsLoading, setSlotsLoading] = useState(false);
  const [slotsLoaded, setSlotsLoaded] = useState(false);

  const load = async () => {
    try {
      const [appointments, allBarbers, allBranches, allServices, tenantConfig] = await Promise.all([
        apiClient.get<Appointment[]>("/v1/admin/appointments"),
        apiClient.get<Barber[]>("/v1/admin/barbers"),
        apiClient.get<Branch[]>("/v1/admin/branches"),
        apiClient.get<CatalogService[]>("/v1/admin/services"),
        apiClient.get<TenantConfig>("/v1/public/config"),
      ]);
      setItems(appointments); setBarbers(allBarbers); setBranches(allBranches); setServices(allServices); setConfig(tenantConfig);
    } catch (cause) { setError(cause); }
  };
  useEffect(() => { void load(); }, []);

  const availableBarbers  = useMemo(() => barbers.filter((b) => b.active && (!form.branch_id || b.branch_ids?.includes(form.branch_id))), [barbers, form.branch_id]);
  const availableServices = useMemo(() => services.filter((s) => s.active), [services]);

  const calendarDays = useMemo(() => {
    const cur = new Date(`${calendarDate}T12:00:00`);
    if (view === "day") return [cur];
    const start = monday(cur);
    return Array.from({ length: 7 }, (_, i) => { const d = new Date(start); d.setDate(start.getDate() + i); return d; });
  }, [calendarDate, view]);

  const timezone = config?.business_timezone ?? "UTC";
  const calendarItems = useMemo(
    () => items?.filter((a) => calendarDays.some((d) => dateKeyInTimezone(a.start_at, timezone) === isoDate(d))) ?? [],
    [items, calendarDays, timezone],
  );

  async function loadSlots() {
    if (!form.barber_id || !form.service_id || !form.date) return;
    setFormError(""); setSlotsLoading(true); setSlotsLoaded(false);
    try {
      const q = new URLSearchParams({ barber_id: form.barber_id, service_id: form.service_id, date: form.date });
      const response = await apiClient.get<AvailabilitySlot[]>(`/v1/admin/availability?${q.toString()}`);
      setSlots(response); setSlotsLoaded(true);
      if (!response.some((s) => s.start_at === form.start_at)) setForm((f) => ({ ...f, start_at: "" }));
    } catch (cause) { setSlots([]); setFormError(cause instanceof ApiError ? cause.message : "Uygun saatler yüklenemedi."); }
    finally { setSlotsLoading(false); }
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!editable || !form.start_at) return; setFormError(""); setSaving(true);
    try {
      await apiClient.post<Appointment>("/v1/admin/appointments", { branch_id: form.branch_id, barber_id: form.barber_id, service_id: form.service_id, customer_name: form.customer_name, customer_contact: form.customer_contact, start_at: form.start_at }, { headers: { "Idempotency-Key": crypto.randomUUID() } });
      setForm(emptyForm()); setSlots([]); setShowCreate(false); await load();
    } catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Randevu oluşturulamadı."); }
    finally { setSaving(false); }
  }

  async function transition(item: Appointment, action: "confirm" | "cancel" | "complete" | "no-show") {
    if (!editable) return; setSaving(true);
    try { await apiClient.post(`/v1/admin/appointments/${item.id}/${action}`); await load(); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Randevu güncellenemedi."); }
    finally { setSaving(false); setCancelTarget(undefined); }
  }

  async function reschedule(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!editing || !form.start_at) return; setSaving(true);
    try { await apiClient.post(`/v1/admin/appointments/${editing.id}/reschedule`, { start_at: form.start_at }); setEditing(undefined); setForm(emptyForm()); setSlots([]); await load(); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Randevu yeniden planlanamadı."); }
    finally { setSaving(false); }
  }

  function beginReschedule(item: Appointment) {
    setEditing(item);
    setForm({ branch_id: item.branch_id, barber_id: item.barber_id, service_id: item.service_id, customer_name: item.customer_name, customer_contact: item.customer_contact, date: dateKeyInTimezone(item.start_at, timezone), start_at: "" });
    setSlots([]); setFormError("");
  }

  function navigate(delta: number) {
    const d = new Date(`${calendarDate}T12:00:00`);
    d.setDate(d.getDate() + (view === "week" ? delta * 7 : delta));
    setCalendarDate(isoDate(d));
  }

  if (error) return <><PageHeader title="Randevular" /><ErrorNotice error={error} onRetry={() => { setError(undefined); void load(); }} /></>;
  if (!items || !config) return <><PageHeader title="Randevular" description="İşletmenizin randevularını oluşturun ve yönetin." /><LoadingState /></>;

  const tz = config.business_timezone;

  return (
    <>
      <PageHeader
        title="Randevular"
        description={`Uygunluk saatleri ${tz} zaman diliminde gösterilir.`}
        action={editable ? <button className="button primary" onClick={() => { setShowCreate(true); setForm(emptyForm()); setSlots([]); setFormError(""); }}>+ Yeni randevu</button> : undefined}
      />

      {formError && <div className="notice error" style={{ marginBottom: "1rem" }}>{formError}</div>}

      {/* Calendar */}
      <section className="panel">
        <div className="panel-header">
          <div className="panel-header-text"><h2>Takvim</h2></div>
          <div className="cal-header" style={{ margin: 0 }}>
            <div className="button-row">
              <button className={`button sm${view === "day" ? " primary" : " secondary"}`} onClick={() => setView("day")}>Gün</button>
              <button className={`button sm${view === "week" ? " primary" : " secondary"}`} onClick={() => setView("week")}>Hafta</button>
            </div>
            <div className="cal-nav">
              <button className="button ghost sm icon" aria-label="Önceki" onClick={() => navigate(-1)}>‹</button>
              <input aria-label="Takvim tarihi" type="date" value={calendarDate} onChange={(e) => setCalendarDate(e.target.value)} style={{ width: "auto", minWidth: "9rem" }} />
              <button className="button ghost sm icon" aria-label="Sonraki" onClick={() => navigate(1)}>›</button>
            </div>
          </div>
        </div>

        <div className={`calendar ${view}`}>
          {calendarDays.map((day) => {
            const dayItems = calendarItems
              .filter((a) => dateKeyInTimezone(a.start_at, tz) === isoDate(day))
              .sort((a, b) => a.start_at.localeCompare(b.start_at));
            return (
              <section className="calendar-day" key={day.toISOString()}>
                <h3>{new Intl.DateTimeFormat("tr-TR", { weekday: "short", month: "short", day: "numeric" }).format(day)}</h3>
                {dayItems.length === 0 && <p style={{ color: "var(--muted)", fontSize: ".75rem" }}>Randevu yok</p>}
                {dayItems.map((a) => (
                  <article className={`appt-card${a.status === "cancelled" ? " cancelled" : a.status === "completed" ? " completed" : a.status === "no_show" ? " no-show" : ""}`} key={a.id}>
                    <strong>{a.customer_name}</strong>
                    <span>{displayDateTime(a.start_at, tz)}</span>
                    <StatusBadge label={appointmentStatusLabels[a.status]} active={a.status === "pending" || a.status === "confirmed"} />
                  </article>
                ))}
              </section>
            );
          })}
        </div>
      </section>

      {/* Appointment list */}
      <section className="panel">
        <div className="panel-header">
          <div className="panel-header-text"><h2>Tüm randevular</h2><p className="muted">Toplam {items.length}</p></div>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Müşteri</th>
                <th>Saat</th>
                <th>Durum</th>
                {editable && <th style={{ width: "3rem" }} />}
              </tr>
            </thead>
            <tbody>
              {items.length === 0 ? (
                <tr><td colSpan={editable ? 4 : 3}><EmptyState title="Henüz randevu yok" body="Şube, berber, hizmet ve çalışma planını ayarladıktan sonra randevu oluşturabilirsiniz." /></td></tr>
              ) : (
                [...items].sort((a, b) => b.start_at.localeCompare(a.start_at)).map((a) => {
                  const menuItems = [
                    ...(a.status === "pending" ? [{ label: "Onayla", onClick: () => void transition(a, "confirm") }] : []),
                    ...((a.status === "pending" || a.status === "confirmed") ? [{ label: "Yeniden planla", onClick: () => { beginReschedule(a); } }] : []),
                    ...(a.status === "confirmed" ? [{ label: "Tamamla", onClick: () => void transition(a, "complete") }] : []),
                    ...(a.status === "confirmed" ? [{ label: "Gelmedi", onClick: () => void transition(a, "no-show") }] : []),
                    ...((a.status === "pending" || a.status === "confirmed") ? [{ label: "İptal et", onClick: () => setCancelTarget(a), variant: "danger" as const }] : []),
                  ];
                  return (
                    <tr key={a.id}>
                      <td>
                        <strong>{a.customer_name}</strong>
                        <span className="sub">{a.customer_contact}</span>
                      </td>
                      <td>{displayDateTime(a.start_at, tz)}</td>
                      <td>
                        <StatusBadge label={appointmentStatusLabels[a.status]} active={a.status === "pending" || a.status === "confirmed"} />
                      </td>
                      {editable && <td className="actions">{menuItems.length > 0 && <ActionMenu items={menuItems} />}</td>}
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* Create drawer */}
      {showCreate && (
        <Drawer
          title="Yeni randevu"
          onClose={() => { setShowCreate(false); setForm(emptyForm()); setSlots([]); setFormError(""); }}
          footer={
            <>
              <button className="button secondary" onClick={() => { setShowCreate(false); setForm(emptyForm()); setSlots([]); }} type="button">Vazgeç</button>
              <button className="button primary" disabled={!form.start_at || saving} form="appt-create-form" type="submit">{saving ? "Oluşturuluyor…" : "Randevu oluştur"}</button>
            </>
          }
        >
          <form id="appt-create-form" onSubmit={create} className="form-stack">
            <p className="muted" style={{ fontSize: ".8125rem" }}>Saatler, yetkili uygunluk servisi tarafından {tz} zaman diliminde sunulur.</p>
            <label>Müşteri adı<input value={form.customer_name} onChange={(e) => setForm({ ...form, customer_name: e.target.value })} required /></label>
            <label>İletişim<input value={form.customer_contact} onChange={(e) => setForm({ ...form, customer_contact: e.target.value })} required /></label>
            <label>Şube
              <select value={form.branch_id} onChange={(e) => setForm({ ...form, branch_id: e.target.value, barber_id: "", start_at: "" })} required>
                <option value="">Şube seçin</option>
                {branches.filter((b) => b.active).map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
              </select>
            </label>
            <label>Berber
              <select value={form.barber_id} onChange={(e) => setForm({ ...form, barber_id: e.target.value, start_at: "" })} required>
                <option value="">Berber seçin</option>
                {availableBarbers.map((b) => <option key={b.id} value={b.id}>{b.display_name}</option>)}
              </select>
            </label>
            <label>Hizmet
              <select value={form.service_id} onChange={(e) => setForm({ ...form, service_id: e.target.value, start_at: "" })} required>
                <option value="">Hizmet seçin</option>
                {availableServices.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </label>
            <label>Tarih<input type="date" value={form.date} onChange={(e) => setForm({ ...form, date: e.target.value, start_at: "" })} required /></label>
            <div className="slot-action">
              <button className="button secondary sm" type="button" onClick={() => void loadSlots()} disabled={!form.barber_id || !form.service_id || slotsLoading}>{slotsLoading ? "Saatler yükleniyor…" : "Uygun saatleri yükle"}</button>
              {slots.length > 0 && (
                <select value={form.start_at} onChange={(e) => setForm({ ...form, start_at: e.target.value })} required>
                  <option value="">Saat seçin</option>
                  {slots.map((s) => <option key={s.start_at} value={s.start_at}>{displayDateTime(s.start_at, tz)}</option>)}
                </select>
              )}
              {slotsLoaded && slots.length === 0 && <p className="muted" role="status">Bu tarih için uygun saat yok.</p>}
            </div>
            <FormError value={formError} />
          </form>
        </Drawer>
      )}

      {/* Reschedule modal */}
      {editing && (
        <Modal title={`Yeniden planla: ${editing.customer_name}`} onClose={() => { setEditing(undefined); setForm(emptyForm()); setSlots([]); }}>
          <form className="form-stack" onSubmit={reschedule}>
            <p className="muted" style={{ fontSize: ".8125rem" }}>{tz} zaman diliminde yeni bir uygun saat seçin.</p>
            <label>Tarih<input type="date" value={form.date} onChange={(e) => setForm({ ...form, date: e.target.value, start_at: "" })} required /></label>
            <div className="slot-action">
              <button className="button secondary sm" type="button" onClick={() => void loadSlots()} disabled={!form.barber_id || !form.service_id || slotsLoading}>{slotsLoading ? "Saatler yükleniyor…" : "Saatleri yükle"}</button>
              {slots.length > 0 && (
                <select value={form.start_at} onChange={(e) => setForm({ ...form, start_at: e.target.value })} required>
                  <option value="">Saat seçin</option>
                  {slots.map((s) => <option key={s.start_at} value={s.start_at}>{displayDateTime(s.start_at, tz)}</option>)}
                </select>
              )}
              {slotsLoaded && slots.length === 0 && <p className="muted" role="status">Bu tarih için uygun saat yok.</p>}
            </div>
            <FormError value={formError} />
            <div className="dialog-actions">
              <button className="button secondary" onClick={() => { setEditing(undefined); setForm(emptyForm()); setSlots([]); }} type="button">Vazgeç</button>
              <button className="button primary" disabled={!form.start_at || saving}>{saving ? "Yeniden planlanıyor…" : "Yeniden planla"}</button>
            </div>
          </form>
        </Modal>
      )}

      {/* Cancel confirm */}
      {cancelTarget && (
        <ConfirmDialog
          title="Randevu iptal edilsin mi?"
          description={`${cancelTarget.customer_name} adlı müşterinin randevusu iptal edilsin mi? Bu işlem geri alınamaz.`}
          confirmLabel="Randevuyu iptal et"
          busy={saving}
          onCancel={() => setCancelTarget(undefined)}
          onConfirm={() => void transition(cancelTarget, "cancel")}
        />
      )}
    </>
  );
}
