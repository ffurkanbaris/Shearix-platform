"use client";

import { FormEvent, useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import type { Member, Role } from "@/lib/types";
import { canManageMembers, roleLabels, staffRoles } from "@/lib/types";
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
    catch (cause) { setCreateError(cause instanceof ApiError ? cause.message : "Personel hesabı oluşturulamadı."); return false; }
    finally { setSaving(false); }
  }

  async function saveRole(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!editing) return; setEditError(""); setSaving(true);
    try { await apiClient.patch(`/v1/admin/members/${editing.identity_id}/role`, { role: editRole }); closeEdit(); await load(); }
    catch (cause) { setEditError(cause instanceof ApiError ? cause.message : "Rol değiştirilemedi."); }
    finally { setSaving(false); }
  }

  async function changeStatus() {
    if (!statusTarget) return; setSaving(true);
    try {
      await apiClient.post(`/v1/admin/members/${statusTarget.identity_id}/${statusTarget.status === "active" ? "deactivate" : "activate"}`);
      setStatusTarget(undefined); await load();
    } catch (cause) {
      setCreateError(cause instanceof ApiError ? cause.message : "Personel durumu güncellenemedi.");
      setStatusTarget(undefined);
    } finally { setSaving(false); }
  }

  if (!allowed) {
    return (
      <>
        <PageHeader title="Personel" description="İşletme üyelikleri işletme sahibi tarafından yönetilir." />
        <div className="notice muted">Personel üyeliklerini yalnızca İŞLETME SAHİBİ oluşturabilir veya yönetebilir. Backend yetkilendirmesi her istekte uygulanmaya devam eder.</div>
      </>
    );
  }

  return (
    <>
      <PageHeader
        title="Personel"
        description="YÖNETİCİ, BERBER ve RESEPSİYONİST üyelikleri oluşturun. İŞLETME SAHİBİ hesapları yalnızca platform tarafından sağlanır."
        action={<button className="button primary" onClick={() => { setShowCreate(true); setCreateError(""); }} type="button">+ Personel ekle</button>}
      />

      {error && <ErrorNotice error={error} onRetry={() => { setError(undefined); void load(); }} />}

      <section className="panel">
        <span className="eyebrow">Erişim modeli</span>
        <h2>Personelin erişimi işletmeyle sınırlıdır.</h2>
        <p className="muted">İŞLETME SAHİBİ standart personel rollerini yönetebilir. BERBER ve RESEPSİYONİST rol atayamaz. Mevcut kimlikler başka bir işletmeye eklendiğinde adlarını ve giriş bilgilerini korur.</p>
      </section>

      {/* Members table */}
      <section className="panel">
        <div className="panel-header">
          <div className="panel-header-text">
            <h2>Üyeler</h2>
            {members && <p className="muted">{members.length} işletme üyesi</p>}
          </div>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Ad</th>
                <th>E-posta</th>
                <th>Rol</th>
                <th>Durum</th>
                <th>İşlemler</th>
              </tr>
            </thead>
            <tbody>
              {!members ? (
                <SkeletonRows cols={5} rows={5} />
              ) : members.length === 0 ? (
                <tr><td colSpan={5}><EmptyState title="Henüz personel yok" body="Bir kişiye panel erişimi vermek için personel üyeliği oluşturun." /></td></tr>
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
                          <button className="button secondary sm" onClick={() => openEdit(member)} type="button">Rolü düzenle</button>
                          <button
                            className={`button sm${member.status === "active" ? " danger" : " secondary"}`}
                            onClick={() => setStatusTarget(member)}
                            type="button"
                          >
                            {member.status === "active" ? "Pasife al" : "Etkinleştir"}
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
          title="Personel ekle"
		  onClose={() => { setShowCreate(false); setForm({ name: "", email: "", role: "RECEPTIONIST" }); setCreateError(""); }}
		  footer={<><button className="button secondary" disabled={saving} onClick={() => { setShowCreate(false); setForm({ name: "", email: "", role: "RECEPTIONIST" }); setCreateError(""); }} type="button">Vazgeç</button><button className="button primary" disabled={saving} form="staff-create-form" type="submit">{saving ? "Oluşturuluyor…" : "Personel oluştur"}</button></>}
        >
          <form id="staff-create-form" onSubmit={async (event) => { if (await create(event)) setShowCreate(false); }} className="form-stack">
			<p className="muted">İlk giriş şifresi personelin e-posta adresine gönderilecek.</p>
            <label>Ad<input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} maxLength={160} required /></label>
			<label>E-posta<input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} type="email" autoComplete="email" required /></label>
            <label>Rol
              <select value={form.role} onChange={(e) => setForm({ ...form, role: e.target.value as Role })}>
                {staffRoles.map((r) => <option key={r} value={r}>{roleLabels[r]}</option>)}
              </select>
            </label>
            <FormError value={createError} />
          </form>
        </Drawer>
      )}

      {/* Edit role modal */}
      {editing && (
        <Modal title={`Rolü düzenle: ${editing.name}`} onClose={closeEdit}>
          <form className="form-stack" onSubmit={saveRole}>
			<p className="muted">{editing.email}</p>
            <label>Rol
              <select value={editRole} onChange={(e) => setEditRole(e.target.value as Role)}>
                {staffRoles.map((r) => <option key={r} value={r}>{roleLabels[r]}</option>)}
              </select>
            </label>
            <FormError value={editError} />
            <div className="dialog-actions">
              <button className="button secondary" disabled={saving} onClick={closeEdit} type="button">Vazgeç</button>
              <button className="button primary" disabled={saving}>{saving ? "Kaydediliyor…" : "Rolü kaydet"}</button>
            </div>
          </form>
        </Modal>
      )}

      {/* Status confirm */}
      {statusTarget && (
        <ConfirmDialog
          title={statusTarget.status === "active" ? "Personel pasife alınsın mı?" : "Personel etkinleştirilsin mi?"}
          description={statusTarget.status === "active"
            ? `${statusTarget.name} bu işletmenin paneline erişimini hemen kaybedecek.`
            : `${statusTarget.name} bu işletmenin paneline yeniden erişebilecek.`}
          confirmLabel={statusTarget.status === "active" ? "Personeli pasife al" : "Personeli etkinleştir"}
          variant={statusTarget.status === "active" ? "danger" : "warning"}
          busy={saving}
          onCancel={() => setStatusTarget(undefined)}
          onConfirm={() => void changeStatus()}
        />
      )}
    </>
  );
}
