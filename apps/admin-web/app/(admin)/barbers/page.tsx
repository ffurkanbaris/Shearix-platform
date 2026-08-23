"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import type { Barber, Branch, Member } from "@/lib/types";
import { canManageMembers, canWrite } from "@/lib/types";
import { useSession } from "@/components/auth-gate";
import {
  ActionMenu,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorNotice,
  FormError,
  Modal,
  PageHeader,
  SkeletonRows,
  StatusBadge,
} from "@/components/ui";

type BarberForm = { display_name: string; bio: string; active: boolean; branch_ids: string[] };
const emptyForm: BarberForm = { display_name: "", bio: "", active: true, branch_ids: [] };
const formFor = (barber: Barber): BarberForm => ({ display_name: barber.display_name, bio: barber.bio, active: barber.active, branch_ids: barber.branch_ids ?? [] });

function toggleBranch(form: BarberForm, id: string): BarberForm {
  return { ...form, branch_ids: form.branch_ids.includes(id) ? form.branch_ids.filter((b) => b !== id) : [...form.branch_ids, id] };
}

export default function BarbersPage() {
  const { principal } = useSession();
  const write   = canWrite(principal.role);
  const canLink = canManageMembers(principal.role);
  const [barbers, setBarbers] = useState<Barber[]>();
  const [branches, setBranches] = useState<Branch[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [showCreate, setShowCreate] = useState(false);
  const [createForm, setCreateForm] = useState<BarberForm>(emptyForm);
  const [editing, setEditing] = useState<Barber>();
  const [editForm, setEditForm] = useState<BarberForm>(emptyForm);
  const [linking, setLinking] = useState<Barber>();
  const [identityID, setIdentityID] = useState("");
  const [unlinkTarget, setUnlinkTarget] = useState<Barber>();
  const [statusTarget, setStatusTarget] = useState<Barber>();
  const [error, setError] = useState<unknown>();
  const [createError, setCreateError] = useState("");
  const [editError, setEditError] = useState("");
  const [actionError, setActionError] = useState("");
  const [saving, setSaving] = useState(false);

  const barberMembers = useMemo(() => members.filter((m) => m.role === "BARBER" && m.status === "active"), [members]);

  const load = async () => {
    try {
      setError(undefined);
      const [allBarbers, allBranches] = await Promise.all([
        apiClient.get<Barber[]>("/v1/admin/barbers"),
        apiClient.get<Branch[]>("/v1/admin/branches"),
      ]);
      setBarbers(allBarbers); setBranches(allBranches);
      if (canLink) setMembers(await apiClient.get<Member[]>("/v1/admin/members"));
    } catch (cause) { setError(cause); }
  };
  useEffect(() => { void load(); }, []);

  const closeCreate = () => { setShowCreate(false); setCreateForm(emptyForm); setCreateError(""); };
  const closeEdit   = () => { setEditing(undefined); setEditForm(emptyForm); setEditError(""); };
  const openEdit    = (barber: Barber) => { setEditing(barber); setEditForm(formFor(barber)); setEditError(""); };
  const closeLink   = () => { setLinking(undefined); setIdentityID(""); setActionError(""); };

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setCreateError(""); setSaving(true);
    try { await apiClient.post("/v1/admin/barbers", createForm); closeCreate(); await load(); }
    catch (cause) { setCreateError(cause instanceof ApiError ? cause.message : "Unable to create the barber."); }
    finally { setSaving(false); }
  }

  async function saveEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!editing) return; setEditError(""); setSaving(true);
    try { await apiClient.patch(`/v1/admin/barbers/${editing.id}`, editForm); closeEdit(); await load(); }
    catch (cause) { setEditError(cause instanceof ApiError ? cause.message : "Unable to update the barber."); }
    finally { setSaving(false); }
  }

  async function changeStatus() {
    if (!statusTarget) return; setSaving(true);
    try { await apiClient.patch(`/v1/admin/barbers/${statusTarget.id}`, { ...formFor(statusTarget), active: !statusTarget.active }); setStatusTarget(undefined); await load(); }
    catch (cause) { setActionError(cause instanceof ApiError ? cause.message : "Unable to update barber status."); setStatusTarget(undefined); }
    finally { setSaving(false); }
  }

  async function link(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!linking || !identityID) return; setActionError(""); setSaving(true);
    try { await apiClient.post(`/v1/admin/barbers/${linking.id}/link-identity`, { identity_id: identityID }); closeLink(); await load(); }
    catch (cause) { setActionError(cause instanceof ApiError ? cause.message : "Unable to link this identity."); }
    finally { setSaving(false); }
  }

  async function unlink() {
    if (!unlinkTarget) return; setSaving(true);
    try { await apiClient.delete(`/v1/admin/barbers/${unlinkTarget.id}/link-identity`); setUnlinkTarget(undefined); await load(); }
    catch (cause) { setActionError(cause instanceof ApiError ? cause.message : "Unable to remove the identity link."); setUnlinkTarget(undefined); }
    finally { setSaving(false); }
  }

  const BranchFields = ({ form, setForm }: { form: BarberForm; setForm: (f: BarberForm) => void }) => (
    <fieldset>
      <legend>Branch assignments</legend>
      <div className="check-grid" style={{ marginTop: ".5rem" }}>
        {branches.map((b) => (
          <label className="checkbox" key={b.id}>
            <input type="checkbox" checked={form.branch_ids.includes(b.id)} onChange={() => setForm(toggleBranch(form, b.id))} disabled={!b.active} />
            {b.name}{!b.active && " (inactive)"}
          </label>
        ))}
      </div>
    </fieldset>
  );

  return (
    <>
      <PageHeader
        title="Barbers"
        description="A barber record is separate from a staff identity. Only an eligible BARBER membership may be linked."
        action={write ? <button className="button primary" onClick={() => setShowCreate(true)}>+ Add barber</button> : undefined}
      />

      {error && <ErrorNotice error={error} onRetry={() => { setError(undefined); void load(); }} />}
      {actionError && <div className="notice error" style={{ marginBottom: "1rem" }}>{actionError}</div>}

      <section className="panel">
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Barber</th>
                <th>Branches</th>
                <th>Panel access</th>
                <th>Status</th>
                {(write || canLink) && <th style={{ width: "3rem" }} />}
              </tr>
            </thead>
            <tbody>
              {!barbers ? (
                <SkeletonRows cols={5} rows={5} />
              ) : barbers.length === 0 ? (
                <tr><td colSpan={5}><EmptyState title="No barbers" body="Create a barber before assigning services or working hours." /></td></tr>
              ) : (
                barbers.map((barber) => {
                  const branchCount = barber.branch_ids?.length ?? 0;
                  const menuItems = [
                    ...(write ? [{ label: "Edit", onClick: () => openEdit(barber) }] : []),
                    ...(write ? [{ label: barber.active ? "Deactivate" : "Activate", onClick: () => setStatusTarget(barber), variant: (barber.active ? "danger" : "default") as "danger" | "default" }] : []),
                    ...(canLink && barber.identity_id ? [{ label: "Unlink identity", onClick: () => setUnlinkTarget(barber), variant: "danger" as const }] : []),
                    ...(canLink && !barber.identity_id ? [{ label: "Link identity", onClick: () => { setLinking(barber); setActionError(""); } }] : []),
                  ];
                  return (
                    <tr key={barber.id}>
                    <td>
                      <div className="roster-person">
                        <span className="roster-initial" aria-hidden="true">{barber.display_name.slice(0, 1).toUpperCase()}</span>
                        <div><strong>{barber.display_name}</strong><span className="sub">{barber.bio || "Barber profile"}</span></div>
                      </div>
                    </td>
                      <td>
                        <span className="muted">{branchCount === 0 ? "None" : `${branchCount} branch${branchCount === 1 ? "" : "es"}`}</span>
                      </td>
                      <td>
                        {barber.identity_id ? (
                          <span className="indicator">
                            <span className="indicator-dot green" />
                            Linked
                          </span>
                        ) : (
                          <span className="indicator">
                            <span className="indicator-dot gray" />
                            <span className="muted">No access</span>
                          </span>
                        )}
                      </td>
                      <td><StatusBadge active={barber.active} /></td>
                      {(write || canLink) && <td className="actions"><ActionMenu items={menuItems} /></td>}
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
          title="Add barber"
          onClose={closeCreate}
          footer={
            <>
              <button className="button secondary" disabled={saving} onClick={closeCreate} type="button">Cancel</button>
              <button className="button primary" disabled={saving} form="barber-create-form" type="submit">
                {saving ? "Creating…" : "Create barber"}
              </button>
            </>
          }
        >
          <form id="barber-create-form" onSubmit={create} className="form-stack">
            <label>Display name<input value={createForm.display_name} onChange={(e) => setCreateForm({ ...createForm, display_name: e.target.value })} required /></label>
            <label>Bio<textarea value={createForm.bio} onChange={(e) => setCreateForm({ ...createForm, bio: e.target.value })} rows={3} /></label>
            <BranchFields form={createForm} setForm={setCreateForm} />
            <FormError value={createError} />
          </form>
        </Drawer>
      )}

      {/* Edit drawer */}
      {editing && (
        <Drawer
          title={`Edit barber: ${editing.display_name}`}
          onClose={closeEdit}
          footer={
            <>
              <button className="button secondary" disabled={saving} onClick={closeEdit} type="button">Cancel</button>
              <button className="button primary" disabled={saving} form="barber-edit-form" type="submit">
                {saving ? "Saving…" : "Save changes"}
              </button>
            </>
          }
        >
          <form id="barber-edit-form" onSubmit={saveEdit} className="form-stack">
            <label>Display name<input value={editForm.display_name} onChange={(e) => setEditForm({ ...editForm, display_name: e.target.value })} required /></label>
            <label>Bio<textarea value={editForm.bio} onChange={(e) => setEditForm({ ...editForm, bio: e.target.value })} rows={3} /></label>
            <BranchFields form={editForm} setForm={setEditForm} />
            <label className="checkbox">
              <input type="checkbox" checked={editForm.active} onChange={(e) => setEditForm({ ...editForm, active: e.target.checked })} />
              Active
            </label>
            <FormError value={editError} />
          </form>
        </Drawer>
      )}

      {/* Link identity modal */}
      {linking && (
        <Modal title={`Link panel identity: ${linking.display_name}`} onClose={closeLink}>
          <form className="form-stack" onSubmit={link}>
            <label>Eligible BARBER member
              <select value={identityID} onChange={(e) => setIdentityID(e.target.value)} required>
                <option value="">Select a BARBER membership</option>
                {barberMembers.map((m) => (
				  <option value={m.identity_id} key={m.identity_id}>{m.name} · {m.email}</option>
                ))}
              </select>
            </label>
            <FormError value={actionError} />
            <div className="dialog-actions">
              <button className="button secondary" disabled={saving} onClick={closeLink} type="button">Cancel</button>
              <button className="button primary" disabled={saving}>{saving ? "Linking…" : "Link identity"}</button>
            </div>
          </form>
        </Modal>
      )}

      {/* Status confirm */}
      {statusTarget && (
        <ConfirmDialog
          title={`${statusTarget.active ? "Deactivate" : "Activate"} barber?`}
          description={statusTarget.active ? "This barber will no longer be available for new bookings. Existing appointments are preserved." : "This barber will be available for new bookings again."}
          confirmLabel={statusTarget.active ? "Deactivate barber" : "Activate barber"}
          variant={statusTarget.active ? "danger" : "warning"}
          busy={saving}
          onCancel={() => setStatusTarget(undefined)}
          onConfirm={() => void changeStatus()}
        />
      )}

      {/* Unlink confirm */}
      {unlinkTarget && (
        <ConfirmDialog
          title="Unlink panel identity?"
          description={`This removes ${unlinkTarget.display_name}'s panel access link. It does not delete the identity or its tenant membership.`}
          confirmLabel="Unlink identity"
          busy={saving}
          onCancel={() => setUnlinkTarget(undefined)}
          onConfirm={() => void unlink()}
        />
      )}
    </>
  );
}
