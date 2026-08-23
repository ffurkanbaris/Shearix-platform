"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { localDate, toLocalInput } from "@/lib/format";
import type { Barber, BlockedPeriod, Interval, Schedule, ScheduleOverride, WorkingDay } from "@/lib/types";
import { canWrite } from "@/lib/types";
import { useSession } from "@/components/auth-gate";
import { ConfirmDialog, EmptyState, ErrorNotice, FormError, LoadingState, Modal, PageHeader, ScheduleGrid } from "@/components/ui";

const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
type OverrideForm = { date: string; kind: "unavailable" | "vacation" | "custom_hours"; intervals: string };
const emptyOverride = (): OverrideForm => ({ date: localDate(), kind: "unavailable", intervals: "" });

function textIntervals(intervals: Interval[]): string { return intervals.map((i) => `${i.start}-${i.end}`).join(", "); }
function parseIntervals(value: string): Interval[] | null {
  if (!value.trim()) return [];
  const output = value.split(",").map((p) => p.trim()).filter(Boolean).map((p) => { const [start, end, extra] = p.split("-").map((x) => x.trim()); return { start, end, extra }; });
  if (output.some((i) => !i.start || !i.end || i.extra || !/^\d{2}:\d{2}$/.test(i.start) || !/^\d{2}:\d{2}$/.test(i.end) || i.start >= i.end)) return null;
  return output.map(({ start, end }) => ({ start, end }));
}
function validOverride(form: OverrideForm): Interval[] | null {
  const intervals = parseIntervals(form.intervals);
  if ((form.kind === "custom_hours" && (!intervals || intervals.length === 0)) || (form.kind !== "custom_hours" && intervals?.length)) return null;
  return intervals;
}

export default function SchedulesPage() {
  const { principal } = useSession();
  const [allBarbers, setAllBarbers] = useState<Barber[]>();
  const [selectedID, setSelectedID] = useState("");
  const [schedule, setSchedule] = useState<Schedule>();
  const [error, setError] = useState<unknown>();
  const [formError, setFormError] = useState("");
  const [hours, setHours] = useState<Record<number, string>>({});
  const [override, setOverride] = useState<OverrideForm>(emptyOverride);
  const [showAddOverride] = useState(true);
  const [block, setBlock] = useState({ start_at: "", end_at: "" });
  const [showAddBlock] = useState(true);
  const [editingOverride, setEditingOverride] = useState<ScheduleOverride>();
  const [editOverride, setEditOverride] = useState<OverrideForm>(emptyOverride);
  const [deleteOverrideTarget, setDeleteOverrideTarget] = useState<ScheduleOverride>();
  const [deleteBlockTarget, setDeleteBlockTarget] = useState<BlockedPeriod>();
  const [saving, setSaving] = useState(false);

  const barbers = useMemo(
    () => (allBarbers ?? []).filter((b) => principal.role !== "BARBER" || b.identity_id === principal.identity_id),
    [allBarbers, principal],
  );
  const canEdit = canWrite(principal.role) || principal.role === "BARBER";

  const loadSchedule = async (barberID: string) => {
    if (!barberID) return;
    try {
      setError(undefined); setSchedule(undefined);
      const value = await apiClient.get<Schedule>(`/v1/admin/barbers/${barberID}/schedule`);
      setSchedule(value); setHours(Object.fromEntries(value.working_hours.map((d) => [d.weekday, textIntervals(d.intervals)])));
    } catch (cause) { setError(cause); }
  };

  useEffect(() => {
    void apiClient.get<Barber[]>("/v1/admin/barbers").then((items) => {
      setAllBarbers(items);
      const own = items.find((b) => principal.role !== "BARBER" || b.identity_id === principal.identity_id);
      if (own) { setSelectedID(own.id); void loadSchedule(own.id); }
    }).catch(setError);
  }, []);

  const closeEditOverride = () => { setEditingOverride(undefined); setEditOverride(emptyOverride()); setFormError(""); };
  const openEditOverride  = (item: ScheduleOverride) => { setEditingOverride(item); setEditOverride({ date: item.date, kind: item.kind, intervals: textIntervals(item.intervals ?? []) }); setFormError(""); };

  async function saveHours(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!selectedID) return;
    const payload: WorkingDay[] = [];
    for (let d = 0; d < 7; d += 1) {
      const intervals = parseIntervals(hours[d] ?? "");
      if (!intervals) { setFormError(`Use HH:MM-HH:MM for ${DAYS[d]}, separated by commas.`); return; }
      if (intervals.length) payload.push({ weekday: d, intervals });
    }
    setFormError(""); setSaving(true);
    try { await apiClient.put<void>(`/v1/admin/barbers/${selectedID}/working-hours`, payload); await loadSchedule(selectedID); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Unable to save working hours."); }
    finally { setSaving(false); }
  }

  async function addOverride(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!selectedID) return;
    const intervals = validOverride(override);
    if (!intervals) { setFormError("Custom hours need intervals; vacation and unavailable overrides must not include intervals."); return; }
    setFormError(""); setSaving(true);
    try { await apiClient.post(`/v1/admin/barbers/${selectedID}/overrides`, { date: override.date, kind: override.kind, intervals }); setOverride(emptyOverride()); await loadSchedule(selectedID); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Unable to save the override."); }
    finally { setSaving(false); }
  }

  async function saveOverrideEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!selectedID || !editingOverride) return;
    const intervals = validOverride(editOverride);
    if (!intervals) { setFormError("Custom hours need intervals; vacation and unavailable overrides must not include intervals."); return; }
    setFormError(""); setSaving(true);
    try { await apiClient.patch(`/v1/admin/barbers/${selectedID}/overrides/${editingOverride.id}`, { date: editOverride.date, kind: editOverride.kind, intervals }); closeEditOverride(); await loadSchedule(selectedID); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Unable to update the override."); }
    finally { setSaving(false); }
  }

  async function deleteOverride() {
    if (!selectedID || !deleteOverrideTarget) return; setSaving(true);
    try { await apiClient.delete(`/v1/admin/barbers/${selectedID}/overrides/${deleteOverrideTarget.id}`); setDeleteOverrideTarget(undefined); await loadSchedule(selectedID); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Unable to delete the override."); setDeleteOverrideTarget(undefined); }
    finally { setSaving(false); }
  }

  async function addBlock(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!selectedID || !block.start_at || !block.end_at) return;
    setFormError(""); setSaving(true);
    try { await apiClient.post(`/v1/admin/barbers/${selectedID}/blocked-periods`, { start_at: new Date(block.start_at).toISOString(), end_at: new Date(block.end_at).toISOString() }); setBlock({ start_at: "", end_at: "" }); await loadSchedule(selectedID); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Unable to save the blocked period."); }
    finally { setSaving(false); }
  }

  async function deleteBlock() {
    if (!selectedID || !deleteBlockTarget) return; setSaving(true);
    try { await apiClient.delete(`/v1/admin/barbers/${selectedID}/blocked-periods/${deleteBlockTarget.id}`); setDeleteBlockTarget(undefined); await loadSchedule(selectedID); }
    catch (cause) { setFormError(cause instanceof ApiError ? cause.message : "Unable to delete the blocked period."); setDeleteBlockTarget(undefined); }
    finally { setSaving(false); }
  }

  if (error) return <><PageHeader title="Schedules" /><ErrorNotice error={error} onRetry={() => { setError(undefined); void loadSchedule(selectedID); }} /></>;
  if (!allBarbers) return <><PageHeader title="Schedules" description="Configure working hours and exceptions." /><LoadingState /></>;
  if (!barbers.length) return (
    <>
      <PageHeader title="Schedules" description="Configure working hours and exceptions." />
      <EmptyState
        title={principal.role === "BARBER" ? "No linked barber" : "No barbers yet"}
        body={principal.role === "BARBER" ? "Your account needs an eligible barber identity link before you can manage a schedule." : "Create a barber before configuring working hours and exceptions."}
      />
    </>
  );

  return (
    <>
      <PageHeader title="Schedules" description={principal.role === "BARBER" ? "Your linked barber schedule." : "Working hours, date overrides, and blocked periods."} />

      {/* Barber selector */}
      <div className="barber-selector">
        <label style={{ flexDirection: "row", alignItems: "center", gap: ".5rem", margin: 0 }}>
          Barber
          <select value={selectedID} onChange={(e) => { setSelectedID(e.target.value); void loadSchedule(e.target.value); }} style={{ width: "auto", minWidth: "200px" }}>
            {barbers.map((b) => <option key={b.id} value={b.id}>{b.display_name}</option>)}
          </select>
        </label>
      </div>

      {!schedule ? <LoadingState label="Loading schedule…" /> : (
        <>
          {/* Weekly hours */}
          <section className="panel">
            <form onSubmit={saveHours}>
              <div className="panel-header">
                <div className="panel-header-text">
                  <h2>Weekly working hours</h2>
                  <p className="muted">Use one or more intervals, e.g. <code>09:00-12:00, 13:00-18:00</code>. Leave blank to close the day.</p>
                </div>
                {canEdit && <button className="button primary sm" disabled={saving} type="submit">{saving ? "Saving…" : "Save hours"}</button>}
              </div>

              <ScheduleGrid
                days={DAYS.map((label, weekday) => ({
                  label: label.slice(0, 3),
                  intervals: schedule.working_hours.find((item) => item.weekday === weekday)?.intervals ?? [],
                }))}
              />

              {schedule.working_hours.length === 0 && (
                <div className="notice muted" style={{ marginBottom: "1rem" }}>
                  This barber is currently closed every day. Add intervals below to make time bookable.
                </div>
              )}

              <div className="hours-grid">
                {DAYS.map((day, weekday) => (
                  <div className="hours-row" key={day}>
                    <span className="hours-day">{day}</span>
                    <input
                      value={hours[weekday] ?? ""}
                      onChange={(e) => setHours({ ...hours, [weekday]: e.target.value })}
                      placeholder="Closed"
                      disabled={!canEdit || saving}
                    />
                  </div>
                ))}
              </div>
            </form>
          </section>

          {formError && <FormError value={formError} />}

          {/* Two-column: overrides + blocked */}
          <div className="two-col">
            {/* Overrides */}
            <section className="panel">
              <div className="panel-header">
                <div className="panel-header-text">
                  <h2>Date overrides</h2>
                  <p className="muted">Weekly hours apply unless a date override is set.</p>
                </div>

              </div>

              {showAddOverride && canEdit && (
                <form onSubmit={addOverride} className="form-stack" style={{ marginBottom: "1rem", paddingBottom: "1rem", borderBottom: "1px solid var(--line-2)" }}>
                  <OverrideFields form={override} setForm={setOverride} disabled={saving} />
                  <button className="button primary sm" disabled={saving} type="submit">{saving ? "Saving…" : "Add override"}</button>
                </form>
              )}

              {schedule.overrides.length === 0 ? (
                <EmptyState title="No overrides" body="Weekly hours apply unless you add a date-specific override." />
              ) : (
                <div className="list">
                  {schedule.overrides.map((item) => (
                    <div className="list-row" key={item.id}>
                      <div className="list-row-main">
                        <strong>{item.date} · {item.kind}</strong>
                        <span>{item.intervals?.length ? textIntervals(item.intervals) : "All day"}</span>
                      </div>
                      {canEdit && (
                        <div className="list-row-actions">
                          <button className="button xs secondary" onClick={() => openEditOverride(item)} type="button">Edit</button>
                          <button className="button xs danger" onClick={() => setDeleteOverrideTarget(item)} type="button">Delete</button>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </section>

            {/* Blocked periods */}
            <section className="panel">
              <div className="panel-header">
                <div className="panel-header-text">
                  <h2>Blocked periods</h2>
                  <p className="muted">Ad-hoc unavailable time slots.</p>
                </div>

              </div>

              {showAddBlock && canEdit && (
                <form onSubmit={addBlock} className="form-stack" style={{ marginBottom: "1rem", paddingBottom: "1rem", borderBottom: "1px solid var(--line-2)" }}>
                  <label>Start<input type="datetime-local" value={block.start_at} onChange={(e) => setBlock({ ...block, start_at: e.target.value })} disabled={saving} required /></label>
                  <label>End<input type="datetime-local" value={block.end_at} onChange={(e) => setBlock({ ...block, end_at: e.target.value })} disabled={saving} required /></label>
                  <button className="button primary sm" disabled={saving} type="submit">{saving ? "Saving…" : "Block time"}</button>
                </form>
              )}

              {schedule.blocked_periods.length === 0 ? (
                <EmptyState title="No blocked periods" body="Use one for ad-hoc unavailable time." />
              ) : (
                <div className="list">
                  {schedule.blocked_periods.map((item) => (
                    <div className="list-row" key={item.id}>
                      <div className="list-row-main">
                        <strong>{toLocalInput(item.start_at).replace("T", " ")}</strong>
                        <span>to {toLocalInput(item.end_at).replace("T", " ")}</span>
                      </div>
                      {canEdit && (
                        <button className="button xs danger" onClick={() => setDeleteBlockTarget(item)} type="button">Delete</button>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </section>
          </div>
        </>
      )}

      {/* Edit override modal */}
      {editingOverride && (
        <Modal title={`Edit override: ${editingOverride.date}`} onClose={closeEditOverride}>
          <form className="form-stack" onSubmit={saveOverrideEdit}>
            <OverrideFields form={editOverride} setForm={setEditOverride} disabled={saving} />
            <FormError value={formError} />
            <div className="dialog-actions">
              <button className="button secondary" disabled={saving} onClick={closeEditOverride} type="button">Cancel</button>
              <button className="button primary" disabled={saving}>{saving ? "Saving…" : "Save changes"}</button>
            </div>
          </form>
        </Modal>
      )}

      {deleteOverrideTarget && (
        <ConfirmDialog title="Delete schedule override?" description="This date-specific override will be removed and weekly working hours will apply again." confirmLabel="Delete override" busy={saving} onCancel={() => setDeleteOverrideTarget(undefined)} onConfirm={() => void deleteOverride()} />
      )}
      {deleteBlockTarget && (
        <ConfirmDialog title="Delete blocked period?" description="This time will no longer be blocked and may become available for booking." confirmLabel="Delete blocked period" busy={saving} onCancel={() => setDeleteBlockTarget(undefined)} onConfirm={() => void deleteBlock()} />
      )}
    </>
  );
}

function OverrideFields({ form, setForm, disabled }: { form: OverrideForm; setForm: (f: OverrideForm) => void; disabled: boolean }) {
  return (
    <>
      <label>Date<input type="date" value={form.date} onChange={(e) => setForm({ ...form, date: e.target.value })} required disabled={disabled} /></label>
      <label>Type
        <select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as OverrideForm["kind"] })} disabled={disabled}>
          <option value="unavailable">Unavailable</option>
          <option value="vacation">Vacation</option>
          <option value="custom_hours">Custom hours</option>
        </select>
      </label>
      {form.kind === "custom_hours" && (
        <label>Intervals<input value={form.intervals} onChange={(e) => setForm({ ...form, intervals: e.target.value })} placeholder="10:00-14:00, 15:00-18:00" disabled={disabled} /></label>
      )}
    </>
  );
}
