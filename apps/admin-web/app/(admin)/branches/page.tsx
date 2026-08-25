"use client";

import { FormEvent, useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import type { Branch } from "@/lib/types";
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

type BranchForm = { name: string; address: string; active: boolean };
const emptyForm: BranchForm = { name: "", address: "", active: true };
const formFor = (branch: Branch): BranchForm => ({ name: branch.name, address: branch.address, active: branch.active });

export default function BranchesPage() {
  const { principal } = useSession();
  const write = canWrite(principal.role);
  const [items, setItems] = useState<Branch[]>();
  const [showCreate, setShowCreate] = useState(false);
  const [createForm, setCreateForm] = useState<BranchForm>(emptyForm);
  const [editing, setEditing] = useState<Branch>();
  const [editForm, setEditForm] = useState<BranchForm>(emptyForm);
  const [statusTarget, setStatusTarget] = useState<Branch>();
  const [error, setError] = useState<unknown>();
  const [createError, setCreateError] = useState("");
  const [editError, setEditError] = useState("");
  const [saving, setSaving] = useState(false);

  const load = async () => {
    try { setError(undefined); setItems(await apiClient.get<Branch[]>("/v1/admin/branches")); }
    catch (cause) { setError(cause); }
  };
  useEffect(() => { void load(); }, []);

  const closeEdit = () => { setEditing(undefined); setEditForm(emptyForm); setEditError(""); };
  const openEdit  = (branch: Branch) => { setEditing(branch); setEditForm(formFor(branch)); setEditError(""); };

  async function create(event: FormEvent<HTMLFormElement>): Promise<boolean> {
    event.preventDefault(); setCreateError(""); setSaving(true);
    try { await apiClient.post("/v1/admin/branches", createForm); setCreateForm(emptyForm); await load(); return true; }
    catch (cause) { setCreateError(cause instanceof ApiError ? cause.message : "Şube oluşturulamadı."); return false; }
    finally { setSaving(false); }
  }

  async function saveEdit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!editing) return; setEditError(""); setSaving(true);
    try { await apiClient.patch(`/v1/admin/branches/${editing.id}`, editForm); closeEdit(); await load(); }
    catch (cause) { setEditError(cause instanceof ApiError ? cause.message : "Şube güncellenemedi."); }
    finally { setSaving(false); }
  }

  async function changeStatus() {
    if (!statusTarget) return; setSaving(true);
    try {
      await apiClient.patch(`/v1/admin/branches/${statusTarget.id}`, { ...formFor(statusTarget), active: !statusTarget.active });
      setStatusTarget(undefined); await load();
    } catch (cause) {
      setCreateError(cause instanceof ApiError ? cause.message : "Şube durumu güncellenemedi.");
      setStatusTarget(undefined);
    } finally { setSaving(false); }
  }

  return (
    <>
      <PageHeader
        title="Şubeler"
        description="Randevuların gerçekleştiği yerleri yönetin."
        action={write ? <button className="button primary" onClick={() => { setShowCreate(true); setCreateError(""); }} type="button">+ Yeni şube</button> : undefined}
      />

      {error && <ErrorNotice error={error} onRetry={() => { setError(undefined); void load(); }} />}

      {/* Branch list */}
      <section className="panel">
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Ad</th>
                <th>Adres</th>
                <th>Durum</th>
                {write && <th>İşlemler</th>}
              </tr>
            </thead>
            <tbody>
              {!items ? (
                <SkeletonRows cols={write ? 4 : 3} rows={4} />
              ) : items.length === 0 ? (
                <tr>
                  <td colSpan={write ? 4 : 3}>
                    <EmptyState title="Henüz şube yok" body="Berberleri ve randevuları düzenlemek için ilk şubenizi oluşturun." />
                  </td>
                </tr>
              ) : (
                items.map((branch) => (
                  <tr key={branch.id}>
                    <td><strong>{branch.name}</strong></td>
                    <td><span className="muted">{branch.address || "—"}</span></td>
                    <td><StatusBadge active={branch.active} /></td>
                    {write && (
                      <td>
                        <div className="row-actions">
                          <button className="button secondary sm" onClick={() => openEdit(branch)} type="button">Düzenle</button>
                          <button className={`button sm${branch.active ? " danger" : " secondary"}`} onClick={() => setStatusTarget(branch)} type="button">
                            {branch.active ? "Pasife al" : "Etkinleştir"}
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
          title="Yeni şube"
          onClose={() => { setShowCreate(false); setCreateForm(emptyForm); setCreateError(""); }}
          footer={<><button className="button secondary" disabled={saving} onClick={() => { setShowCreate(false); setCreateForm(emptyForm); setCreateError(""); }} type="button">Vazgeç</button><button className="button primary" disabled={saving} form="branch-create-form" type="submit">{saving ? "Oluşturuluyor…" : "Şube oluştur"}</button></>}
        >
          <form id="branch-create-form" onSubmit={async (event) => { if (await create(event)) setShowCreate(false); }} className="form-stack">
            <label>Ad<input value={createForm.name} onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })} required /></label>
            <label>Adres<input value={createForm.address} onChange={(e) => setCreateForm({ ...createForm, address: e.target.value })} /></label>
            <FormError value={createError} />
          </form>
        </Drawer>
      )}

      {/* Edit modal */}
      {editing && (
        <Modal title={`Edit branch: ${editing.name}`} onClose={closeEdit}>
          <form className="form-stack" onSubmit={saveEdit}>
            <label>Ad<input value={editForm.name} onChange={(e) => setEditForm({ ...editForm, name: e.target.value })} required /></label>
            <label>Adres<input value={editForm.address} onChange={(e) => setEditForm({ ...editForm, address: e.target.value })} /></label>
            <label className="checkbox">
              <input type="checkbox" checked={editForm.active} onChange={(e) => setEditForm({ ...editForm, active: e.target.checked })} />
              Active
            </label>
            <FormError value={editError} />
            <div className="dialog-actions">
              <button className="button secondary" disabled={saving} onClick={closeEdit} type="button">Vazgeç</button>
              <button className="button primary" disabled={saving}>{saving ? "Kaydediliyor…" : "Değişiklikleri kaydet"}</button>
            </div>
          </form>
        </Modal>
      )}

      {/* Status confirm */}
      {statusTarget && (
        <ConfirmDialog
          title={statusTarget.active ? "Şube pasife alınsın mı?" : "Şube etkinleştirilsin mi?"}
          description={statusTarget.active
            ? "Bu şube yeni randevular için kullanılamayacak. Mevcut randevular korunacak."
            : "Bu şube yeniden yeni randevular için kullanılabilecek."}
          confirmLabel={statusTarget.active ? "Şubeyi pasife al" : "Şubeyi etkinleştir"}
          variant={statusTarget.active ? "danger" : "warning"}
          busy={saving}
          onCancel={() => setStatusTarget(undefined)}
          onConfirm={() => void changeStatus()}
        />
      )}
    </>
  );
}
