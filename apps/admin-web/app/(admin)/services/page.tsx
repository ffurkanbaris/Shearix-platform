"use client";

import { FormEvent, useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { currency } from "@/lib/format";
import type { Barber, CatalogService } from "@/lib/types";
import { canWrite } from "@/lib/types";
import { useSession } from "@/components/auth-gate";
import {
  ConfirmDialog,
  EmptyState,
  Drawer,
  ErrorNotice,
  FormError,
  Modal,
  PageHeader,
  SkeletonRows,
  StatusBadge,
} from "@/components/ui";

type ServiceForm = {
  name: string; price: string; currency: string;
  duration_minutes: number; buffer_before_minutes: number; buffer_after_minutes: number;
  active: boolean;
};
const emptyForm: ServiceForm = { name: "", price: "", currency: "TRY", duration_minutes: 30, buffer_before_minutes: 0, buffer_after_minutes: 0, active: true };
const formFor = (s: CatalogService): ServiceForm => ({ name: s.name, price: s.price, currency: s.currency, duration_minutes: s.duration_minutes, buffer_before_minutes: s.buffer_before_minutes, buffer_after_minutes: s.buffer_after_minutes, active: s.active });

function ServiceFields({ form, setForm, includeActive }: { form: ServiceForm; setForm: (f: ServiceForm) => void; includeActive: boolean }) {
  return (
    <>
      <label>Name<input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required /></label>
      <div className="form-grid">
        <label>Price<input value={form.price} onChange={(e) => setForm({ ...form, price: e.target.value })} inputMode="decimal" placeholder="250.00" required /></label>
        <label>Currency<input value={form.currency} onChange={(e) => setForm({ ...form, currency: e.target.value.toUpperCase() })} minLength={3} maxLength={3} required /></label>
        <label>Duration (min)<input type="number" min={0} value={form.duration_minutes} onChange={(e) => setForm({ ...form, duration_minutes: Number(e.target.value) })} required /></label>
        <label>Buffer before (min)<input type="number" min={0} value={form.buffer_before_minutes} onChange={(e) => setForm({ ...form, buffer_before_minutes: Number(e.target.value) })} required /></label>
        <label>Buffer after (min)<input type="number" min={0} value={form.buffer_after_minutes} onChange={(e) => setForm({ ...form, buffer_after_minutes: Number(e.target.value) })} required /></label>
      </div>
      {includeActive && (
        <label className="checkbox">
          <input type="checkbox" checked={form.active} onChange={(e) => setForm({ ...form, active: e.target.checked })} />
          Active
        </label>
      )}
    </>
  );
}

export default function ServicesPage() {
  const { principal } = useSession();
  const write = canWrite(principal.role);
  const [services, setServices] = useState<CatalogService[]>();
  const [barbers, setBarbers] = useState<Barber[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [createForm, setCreateForm] = useState<ServiceForm>(emptyForm);
  const [editing, setEditing] = useState<CatalogService>();
  const [editForm, setEditForm] = useState<ServiceForm>(emptyForm);
  const [statusTarget, setStatusTarget] = useState<CatalogService>();
  const [assignmentBarber, setAssignmentBarber] = useState<Barber>();
  const [assigned, setAssigned] = useState<CatalogService[]>([]);
  const [unassignTarget, setUnassignTarget] = useState<CatalogService>();
  const [error, setError] = useState<unknown>();
  const [createError, setCreateError] = useState("");
  const [editError, setEditError] = useState("");
  const [actionError, setActionError] = useState("");
  const [saving, setSaving] = useState(false);

  const load = async () => {
    try {
      setError(undefined);
      const [allServices, allBarbers] = await Promise.all([
        apiClient.get<CatalogService[]>("/v1/admin/services"),
        apiClient.get<Barber[]>("/v1/admin/barbers"),
      ]);
      setServices(allServices); setBarbers(allBarbers);
    } catch (cause) { setError(cause); }
  };
  useEffect(() => { void load(); }, []);

  const closeEdit = () => { setEditing(undefined); setEditForm(emptyForm); setEditError(""); };
  const openEdit  = (s: CatalogService) => { setEditing(s); setEditForm(formFor(s)); setEditError(""); };

  async function create(event: FormEvent<HTMLFormElement>): Promise<boolean> {
    event.preventDefault(); setCreateError(""); setSaving(true);
    try { await apiClient.post("/v1/admin/services", createForm); setCreateForm(emptyForm); await load(); return true; }
    catch (cause) { setCreateError(cause instanceof ApiError ? cause.message : "Unable to create the service."); return false; }
    finally { setSaving(false); }
  }

  async function saveEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!editing) return; setEditError(""); setSaving(true);
    try { await apiClient.patch(`/v1/admin/services/${editing.id}`, editForm); closeEdit(); await load(); }
    catch (cause) { setEditError(cause instanceof ApiError ? cause.message : "Unable to update the service."); }
    finally { setSaving(false); }
  }

  async function changeStatus() {
    if (!statusTarget) return; setSaving(true);
    try { await apiClient.patch(`/v1/admin/services/${statusTarget.id}`, { ...formFor(statusTarget), active: !statusTarget.active }); setStatusTarget(undefined); await load(); }
    catch (cause) { setActionError(cause instanceof ApiError ? cause.message : "Unable to update service status."); setStatusTarget(undefined); }
    finally { setSaving(false); }
  }

  async function loadAssignments(barber: Barber) {
    try { setActionError(""); setAssignmentBarber(barber); setAssigned(await apiClient.get<CatalogService[]>(`/v1/admin/barbers/${barber.id}/services`)); }
    catch (cause) { setActionError(cause instanceof ApiError ? cause.message : "Unable to load assignments."); }
  }

  async function assign(serviceID: string) {
    if (!assignmentBarber) return; setSaving(true);
    try { await apiClient.post(`/v1/admin/barbers/${assignmentBarber.id}/services`, { service_id: serviceID }); await loadAssignments(assignmentBarber); }
    catch (cause) { setActionError(cause instanceof ApiError ? cause.message : "Unable to assign this service."); }
    finally { setSaving(false); }
  }

  async function unassign() {
    if (!assignmentBarber || !unassignTarget) return; setSaving(true);
    try { await apiClient.delete(`/v1/admin/barbers/${assignmentBarber.id}/services/${unassignTarget.id}`); setUnassignTarget(undefined); await loadAssignments(assignmentBarber); }
    catch (cause) { setActionError(cause instanceof ApiError ? cause.message : "Unable to remove the assignment."); setUnassignTarget(undefined); }
    finally { setSaving(false); }
  }

  return (
    <>
      <PageHeader
        title="Services"
        description="Prices remain exact decimal strings; duration and booking buffers are validated by the catalog service."
        action={write ? <button className="button primary" onClick={() => { setShowCreate(true); setCreateError(""); }} type="button">+ New service</button> : undefined}
      />

      {error && <ErrorNotice error={error} onRetry={() => { setError(undefined); void load(); }} />}
      {actionError && <div className="notice error" style={{ marginBottom: "1rem" }}>{actionError}</div>}

      {/* Barber assignment manager */}
      {write && services && (
        <section className="panel">
          <div className="panel-header">
            <div className="panel-header-text">
              <h2>Barber assignments</h2>
              <p className="muted">Open a barber to assign the catalog services they can provide.</p>
            </div>
          </div>
          <div className="button-row">
            {barbers.map((b) => (
              <button
                key={b.id}
                className={`button sm${assignmentBarber?.id === b.id ? " primary" : " secondary"}`}
                onClick={() => assignmentBarber?.id === b.id ? (setAssignmentBarber(undefined), setAssigned([])) : void loadAssignments(b)}
                type="button"
              >
                {b.display_name}
              </button>
            ))}
          </div>

          {assignmentBarber && (
            <div style={{ marginTop: "1.25rem" }}>
              <div className="panel-header">
                <div className="panel-header-text">
                  <h2 style={{ fontSize: ".9375rem" }}>{assignmentBarber.display_name}&apos;s services</h2>
                  <p className="muted">Only active, assigned services can be booked.</p>
                </div>
                <button className="button secondary sm" onClick={() => { setAssignmentBarber(undefined); setAssigned([]); }} type="button">Close</button>
              </div>
              <div className="list">
                {services.map((s) => {
                  const isAssigned = assigned.some((a) => a.id === s.id);
                  return (
                    <div className="list-row" key={s.id}>
                      <div className="list-row-main">
                        <strong>{s.name}</strong>
                        <span>{currency(s.price, s.currency)}</span>
                      </div>
                      <button
                        className={`button xs${isAssigned ? " danger" : " secondary"}`}
                        disabled={saving}
                        onClick={() => void (isAssigned ? setUnassignTarget(s) : assign(s.id))}
                        type="button"
                      >
                        {isAssigned ? "Unassign" : "Assign"}
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </section>
      )}

      {/* Service catalog table */}
      <section className="panel">
        <div className="panel-header">
          <div className="panel-header-text"><h2>Service catalog</h2></div>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Service</th>
                <th>Price</th>
                <th>Duration</th>
                <th>Buffers</th>
                <th>Status</th>
                {write && <th>Actions</th>}
              </tr>
            </thead>
            <tbody>
              {!services ? (
                <SkeletonRows cols={write ? 6 : 5} rows={5} />
              ) : services.length === 0 ? (
                <tr><td colSpan={write ? 6 : 5}><EmptyState title="No services" body="Add a service before assigning it to a barber." /></td></tr>
              ) : (
                services.map((s) => (
                  <tr key={s.id}>
                    <td><strong>{s.name}</strong></td>
                    <td>{currency(s.price, s.currency)}</td>
                    <td>{s.duration_minutes} min</td>
                    <td><span className="muted">{s.buffer_before_minutes} / {s.buffer_after_minutes} min</span></td>
                    <td><StatusBadge active={s.active} /></td>
                    {write && (
                      <td>
                        <div className="row-actions">
                          <button className="button secondary sm" onClick={() => openEdit(s)} type="button">Edit</button>
                          <button className={`button sm${s.active ? " danger" : " secondary"}`} onClick={() => setStatusTarget(s)} type="button">
                            {s.active ? "Deactivate" : "Activate"}
                          </button>
                        </div>
                      </td>
                    )}
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      {showCreate && (
        <Drawer
          title="New service"
          onClose={() => { setShowCreate(false); setCreateForm(emptyForm); setCreateError(""); }}
          footer={<><button className="button secondary" disabled={saving} onClick={() => { setShowCreate(false); setCreateForm(emptyForm); setCreateError(""); }} type="button">Cancel</button><button className="button primary" disabled={saving} form="service-create-form" type="submit">{saving ? "Creating…" : "Create service"}</button></>}
        >
          <form id="service-create-form" onSubmit={async (event) => { if (await create(event)) setShowCreate(false); }} className="form-stack">
            <ServiceFields form={createForm} setForm={setCreateForm} includeActive={false} />
            <FormError value={createError} />
          </form>
        </Drawer>
      )}

      {/* Edit modal */}
      {editing && (
        <Modal title={`Edit service: ${editing.name}`} onClose={closeEdit} wide>
          <form className="form-stack" onSubmit={saveEdit}>
            <ServiceFields form={editForm} setForm={setEditForm} includeActive />
            <FormError value={editError} />
            <div className="dialog-actions">
              <button className="button secondary" disabled={saving} onClick={closeEdit} type="button">Cancel</button>
              <button className="button primary" disabled={saving}>{saving ? "Saving…" : "Save changes"}</button>
            </div>
          </form>
        </Modal>
      )}

      {/* Status confirm */}
      {statusTarget && (
        <ConfirmDialog
          title={`${statusTarget.active ? "Deactivate" : "Activate"} service?`}
          description={statusTarget.active ? "This service will no longer be available for new bookings. Existing appointments will not be deleted." : "This service will be available for new bookings again."}
          confirmLabel={statusTarget.active ? "Deactivate service" : "Activate service"}
          variant={statusTarget.active ? "danger" : "warning"}
          busy={saving}
          onCancel={() => setStatusTarget(undefined)}
          onConfirm={() => void changeStatus()}
        />
      )}

      {/* Unassign confirm */}
      {unassignTarget && assignmentBarber && (
        <ConfirmDialog
          title="Remove barber-service assignment?"
          description={`${unassignTarget.name} will no longer be offered by ${assignmentBarber.display_name}. The service remains in the catalog.`}
          confirmLabel="Remove assignment"
          busy={saving}
          onCancel={() => setUnassignTarget(undefined)}
          onConfirm={() => void unassign()}
        />
      )}
    </>
  );
}
