"use client";

import { FormEvent, useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import type { Member, Role } from "@/lib/types";
import { canManageMembers, staffRoles } from "@/lib/types";
import { useSession } from "@/components/auth-gate";
import { normalizeAuthEmail } from "../../../../shared/auth-contract";
import {
  ConfirmDialog,
  EmptyState,
  Drawer,
  ErrorNotice,
  FormError,
  Modal,
  PageHeader,
  RoleBadge,
  SkeletonRows,
  StatusBadge,
} from "@/components/ui";

export default function StaffPage() {
  const { principal } = useSession();
  const [members, setMembers] = useState<Member[]>();
  const [error, setError] = useState<unknown>();
  const [createError, setCreateError] = useState("");
  const [editError, setEditError] = useState("");
  const [form, setForm] = useState({ name: "", email: "", role: "RECEPTIONIST" as Role });
  const [editing, setEditing] = useState<Member>();
  const [editRole, setEditRole] = useState<Role>("RECEPTIONIST");
  const [statusTarget, setStatusTarget] = useState<Member>();
  const [saving, setSaving] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const allowed = canManageMembers(principal.role);

  const load = async () => {
    try { setError(undefined); setMembers(await apiClient.get<Member[]>("/v1/admin/members")); }
    catch (cause) { setError(cause); }
  };
  useEffect(() => { void load(); }, []);

  const closeEdit = () => { setEditing(undefined); setEditRole("RECEPTIONIST"); setEditError(""); };
  const openEdit  = (member: Member) => { setEditing(member); setEditRole(member.role); setEditError(""); };

  async function create(event: FormEvent<HTMLFormElement>): Promise<boolean> {
    event.preventDefault(); setCreateError(""); setSaving(true);
    try { await apiClient.post<Member>("/v1/admin/members", { ...form, name: form.name.trim(), email: normalizeAuthEmail(form.email) }); setForm({ name: "", email: "", role: "RECEPTIONIST" }); await load(); return true; }
    catch (cause) { setCreateError(cause instanceof ApiError ? cause.message : "Unable to create the member."); return false; }
    finally { setSaving(false); }
  }

  async function saveRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!editing) return; setEditError(""); setSaving(true);
    try { await apiClient.patch(`/v1/admin/members/${editing.identity_id}/role`, { role: editRole }); closeEdit(); await load(); }
    catch (cause) { setEditError(cause instanceof ApiError ? cause.message : "Unable to change the role."); }
    finally { setSaving(false); }
  }

  async function changeStatus() {
    if (!statusTarget) return; setSaving(true);
    try {
      await apiClient.post(`/v1/admin/members/${statusTarget.identity_id}/${statusTarget.status === "active" ? "deactivate" : "activate"}`);
      setStatusTarget(undefined); await load();
    } catch (cause) {
      setCreateError(cause instanceof ApiError ? cause.message : "Unable to update member status.");
      setStatusTarget(undefined);
    } finally { setSaving(false); }
  }

  if (!allowed) {
    return (
      <>
        <PageHeader title="Staff" description="Tenant membership is managed by the account owner." />
        <div className="notice muted">Only an OWNER can create or manage staff memberships. Backend authorization remains enforced for every request.</div>
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="Staff"
        description="Create MANAGER, BARBER, and RECEPTIONIST memberships. OWNER accounts are platform-provisioned only."
        action={<button className="button primary" onClick={() => { setShowCreate(true); setCreateError(""); }} type="button">+ Add staff member</button>}
      />

      {error && <ErrorNotice error={error} onRetry={() => { setError(undefined); void load(); }} />}

      <section className="panel">
        <span className="eyebrow">Access model</span>
        <h2>People have tenant-scoped access.</h2>
        <p className="muted">OWNER can manage ordinary staff roles. BARBER and RECEPTIONIST cannot assign roles. Existing identities retain their name and credentials when added to another tenant.</p>
      </section>

      {/* Members table */}
      <section className="panel">
        <div className="panel-header">
          <div className="panel-header-text">
            <h2>Members</h2>
            {members && <p className="muted">{members.length} tenant member{members.length === 1 ? "" : "s"}</p>}
          </div>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Email</th>
                <th>Role</th>
                <th>Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {!members ? (
                <SkeletonRows cols={5} rows={5} />
              ) : members.length === 0 ? (
                <tr><td colSpan={5}><EmptyState title="No staff yet" body="Create a staff membership to give someone panel access." /></td></tr>
              ) : (
                members.map((member) => (
                  <tr key={member.identity_id}>
                    <td><strong>{member.name}</strong></td>
                    <td><span className="muted">{member.email}</span></td>
                    <td><RoleBadge role={member.role} /></td>
                    <td><StatusBadge active={member.status === "active"} /></td>
                    <td>
                      {member.role !== "OWNER" && (
                        <div className="row-actions">
                          <button className="button secondary sm" onClick={() => openEdit(member)} type="button">Edit role</button>
                          <button
                            className={`button sm${member.status === "active" ? " danger" : " secondary"}`}
                            onClick={() => setStatusTarget(member)}
                            type="button"
                          >
                            {member.status === "active" ? "Deactivate" : "Activate"}
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      {showCreate && (
        <Drawer
          title="Add staff member"
		  onClose={() => { setShowCreate(false); setForm({ name: "", email: "", role: "RECEPTIONIST" }); setCreateError(""); }}
		  footer={<><button className="button secondary" disabled={saving} onClick={() => { setShowCreate(false); setForm({ name: "", email: "", role: "RECEPTIONIST" }); setCreateError(""); }} type="button">Cancel</button><button className="button primary" disabled={saving} form="staff-create-form" type="submit">{saving ? "Creating…" : "Create staff member"}</button></>}
        >
          <form id="staff-create-form" onSubmit={async (event) => { if (await create(event)) setShowCreate(false); }} className="form-stack">
			<p className="muted">An initial password will be sent to the staff member&apos;s email address.</p>
            <label>Name<input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} maxLength={160} required /></label>
			<label>Email<input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} type="email" autoComplete="email" required /></label>
            <label>Role
              <select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value as Role })}>
                {staffRoles.map((r) => <option key={r}>{r}</option>)}
              </select>
            </label>
            <FormError value={createError} />
          </form>
        </Drawer>
      )}

      {/* Edit role modal */}
      {editing && (
        <Modal title={`Edit role: ${editing.name}`} onClose={closeEdit}>
          <form className="form-stack" onSubmit={saveRole}>
			<p className="muted">{editing.email}</p>
            <label>Role
              <select value={editRole} onChange={(e) => setEditRole(e.target.value as Role)}>
                {staffRoles.map((r) => <option key={r}>{r}</option>)}
              </select>
            </label>
            <FormError value={editError} />
            <div className="dialog-actions">
              <button className="button secondary" disabled={saving} onClick={closeEdit} type="button">Cancel</button>
              <button className="button primary" disabled={saving}>{saving ? "Saving…" : "Save role"}</button>
            </div>
          </form>
        </Modal>
      )}

      {/* Status confirm */}
      {statusTarget && (
        <ConfirmDialog
          title={`${statusTarget.status === "active" ? "Deactivate" : "Activate"} member?`}
          description={statusTarget.status === "active"
            ? `${statusTarget.name} will immediately lose access to this tenant's panel.`
            : `${statusTarget.name} will regain access to this tenant's panel.`}
          confirmLabel={statusTarget.status === "active" ? "Deactivate member" : "Activate member"}
          variant={statusTarget.status === "active" ? "danger" : "warning"}
          busy={saving}
          onCancel={() => setStatusTarget(undefined)}
          onConfirm={() => void changeStatus()}
        />
      )}
    </>
  );
}
