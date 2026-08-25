"use client";

import { FormEvent, useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import { settingsForm, settingsPayload, type SettingsForm } from "@/lib/settings";
import { canManageTenantSettings, type TenantSettings } from "@/lib/types";
import { useSession } from "@/components/auth-gate";
import { ErrorNotice, FormError, LoadingState, PageHeader } from "@/components/ui";

const emptyForm: SettingsForm = {
  timezone: "",
  booking_interval_minutes: "",
  reminder_offsets_minutes: "",
  cancellation_policy: "allow_until_notice",
  booking_horizon_days: "",
  minimum_booking_notice_minutes: "",
};

export default function SettingsPage() {
  const { principal } = useSession();
  const [settings, setSettings] = useState<TenantSettings>();
  const [form, setForm] = useState<SettingsForm>(emptyForm);
  const [error, setError] = useState<unknown>();
  const [formError, setFormError] = useState("");
  const [saved, setSaved] = useState(false);
  const [saving, setSaving] = useState(false);
  const [passwords, setPasswords] = useState({ current_password: "", new_password: "" });
  const [passwordError, setPasswordError] = useState("");
  const [passwordSaved, setPasswordSaved] = useState(false);
  const [passwordSaving, setPasswordSaving] = useState(false);
  const canEdit = canManageTenantSettings(principal.role);
  const dirty = settings !== undefined && JSON.stringify(form) !== JSON.stringify(settingsForm(settings));

  useEffect(() => {
    const controller = new AbortController();
    void apiClient.get<TenantSettings>("/v1/admin/settings", controller.signal).then((result) => {
      setSettings(result);
      setForm(settingsForm(result));
    }).catch(setError);
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (!dirty) return undefined;
    const beforeUnload = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, [dirty]);

  function update<K extends keyof SettingsForm>(key: K, value: SettingsForm[K]) {
    setSaved(false);
    setForm((cur) => ({ ...cur, [key]: value }));
  }

  async function saveSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setFormError(""); setSaved(false);
    try {
      const payload = settingsPayload(form);
      setSaving(true);
      const result = await apiClient.patch<TenantSettings>("/v1/admin/settings", payload);
      setSettings(result); setForm(settingsForm(result)); setSaved(true);
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : "Ayarlar kaydedilemedi.");
    } finally { setSaving(false); }
  }

  async function changePassword(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setPasswordError(""); setPasswordSaved(false);
    try {
      setPasswordSaving(true);
      await apiClient.post<void>("/v1/admin/auth/change-password", passwords);
      setPasswords({ current_password: "", new_password: "" }); setPasswordSaved(true);
    } catch (cause) {
      setPasswordError(cause instanceof ApiError ? cause.message : "Parola değiştirilemedi.");
    } finally { setPasswordSaving(false); }
  }

  if (error) return <><PageHeader title="Ayarlar" /><ErrorNotice error={error} onRetry={() => window.location.reload()} /></>;
  if (!settings) return <><PageHeader title="Ayarlar" description="İşletme ayarları ve hesap güvenliği." /><LoadingState /></>;

  return (
    <>
      {dirty && canEdit && (
        <div className="unsaved-banner">
          <span>Kaydedilmemiş değişiklikleriniz var.</span>
          <button className="button ghost sm" onClick={() => { setForm(settingsForm(settings)); setFormError(""); }} type="button">Vazgeç</button>
        </div>
      )}

      <PageHeader title="Ayarlar" description="Bu ayarlar yönetim alan adından belirlenen işletmeye aittir." />

      <div className="settings-layout">
        <aside className="settings-nav" aria-label="Ayar bölümleri">
          <span>Genel</span><span>Randevu</span><span>Planlama</span><span>Hatırlatmalar</span><span>İptal</span>
        </aside>
        <div>
          {/* Operational settings */}
          <form className="panel" onSubmit={saveSettings}>
          <div className="panel-header" style={{ marginBottom: "1.5rem" }}>
            <div className="panel-header-text">
              <h2>Randevu ve planlama</h2>
              <p className="muted">Değişiklikler yeni hesaplanan uygunlukları hemen etkiler.</p>
            </div>
          </div>

          <fieldset style={{ border: "none", padding: 0, margin: 0 }}>
            <div className="form-stack">

              <div style={{ borderBottom: "1px solid var(--line-2)", paddingBottom: "1.25rem", marginBottom: ".25rem" }}>
                <p style={{ fontSize: ".6875rem", fontWeight: 600, color: "var(--muted)", textTransform: "uppercase", letterSpacing: ".07em", marginBottom: ".875rem" }}>Genel</p>
                <div className="form-grid">
                  <label style={{ gridColumn: "1 / -1" }}>
                    Timezone
                    <input value={form.timezone} onChange={(e) => update("timezone", e.target.value)} disabled={!canEdit || saving} required placeholder="Europe/Istanbul" />
                  </label>
                  <label>
                    Appointment interval (minutes)
                    <input type="number" min="1" max="120" value={form.booking_interval_minutes} onChange={(e) => update("booking_interval_minutes", e.target.value)} disabled={!canEdit || saving} required />
                  </label>
                  <label>
                    Appointment horizon (days)
                    <input type="number" min="1" max="365" value={form.booking_horizon_days} onChange={(e) => update("booking_horizon_days", e.target.value)} disabled={!canEdit || saving} required />
                  </label>
                  <label>
                    Min. booking notice (minutes)
                    <input type="number" min="0" max="10080" value={form.minimum_booking_notice_minutes} onChange={(e) => update("minimum_booking_notice_minutes", e.target.value)} disabled={!canEdit || saving} required />
                  </label>
                </div>
              </div>

              <div style={{ paddingBottom: ".5rem" }}>
                <p style={{ fontSize: ".6875rem", fontWeight: 600, color: "var(--muted)", textTransform: "uppercase", letterSpacing: ".07em", marginBottom: ".875rem" }}>Hatırlatmalar ve iptal</p>
                <div className="form-grid">
                  <label style={{ gridColumn: "1 / -1" }}>
                    Reminder offsets (minutes before)
                    <input value={form.reminder_offsets_minutes} onChange={(e) => update("reminder_offsets_minutes", e.target.value)} disabled={!canEdit || saving} required aria-describedby="reminder-help" />
                    <span id="reminder-help" className="field-help">Virgülle ayrılmış, birbirinden farklı pozitif değerler girin; ör. 1440, 120.</span>
                  </label>
                  <label style={{ gridColumn: "1 / -1" }}>
                    Cancellation policy
                    <select value={form.cancellation_policy} onChange={(e) => update("cancellation_policy", e.target.value as TenantSettings["cancellation_policy"])} disabled={!canEdit || saving}>
                      <option value="allow_until_notice">Bildirim süresine kadar izin ver</option>
                      <option value="no_cancellation">Müşteri iptaline izin verme</option>
                    </select>
                  </label>
                  <div className="setting-detail" style={{ gridColumn: "1 / -1" }}>
                    <span>İptal bildirim süresi</span>
                    <strong>{settings.cancellation_notice_minutes} minutes</strong>
                    <small style={{ color: "var(--muted)", fontSize: ".75rem" }}>Platform yapılandırmasından yönetilir.</small>
                  </div>
                </div>
              </div>

              <FormError value={formError} />
              {saved && <p className="success-notice">Ayarlar kaydedildi. Yeni uygunluk ve hatırlatma planları bu değerleri kullanacak.</p>}

              {canEdit ? (
                <div className="button-row">
                  <button className="button primary" disabled={saving || !dirty}>{saving ? "Kaydediliyor…" : "Ayarları kaydet"}</button>
                  <button type="button" className="button secondary" disabled={saving || !dirty} onClick={() => { setForm(settingsForm(settings)); setFormError(""); }}>Değişikliklerden vazgeç</button>
                </div>
              ) : (
                <p className="notice muted">Your {principal.role.toLowerCase()} role can view these settings but cannot edit them.</p>
              )}
            </div>
          </fieldset>
          </form>

          {/* Secondary account controls */}
          <div className="two-col">
            {/* Change password */}
            <form className="panel" onSubmit={changePassword}>
            <div className="panel-header">
              <div className="panel-header-text">
                <h2>Parolayı değiştir</h2>
                <p className="muted">Parolanızı yeniler ve diğer aktif oturumları sonlandırır.</p>
              </div>
            </div>
            <div className="form-stack">
              <label>Mevcut parola<input type="password" autoComplete="current-password" value={passwords.current_password} onChange={(e) => setPasswords({ ...passwords, current_password: e.target.value })} required disabled={passwordSaving} /></label>
              <label>Yeni parola<input type="password" autoComplete="new-password" value={passwords.new_password} onChange={(e) => setPasswords({ ...passwords, new_password: e.target.value })} minLength={10} required disabled={passwordSaving} /></label>
              <FormError value={passwordError} />
              {passwordSaved && <p className="success-notice">Parola güncellendi.</p>}
              <button className="button primary" disabled={passwordSaving}>{passwordSaving ? "Parola değiştiriliyor…" : "Parolayı değiştir"}</button>
            </div>
            </form>

            <div>
              {/* Domains */}
              <section className="panel">
            <div className="panel-header">
              <div className="panel-header-text"><h2>Alan adları</h2></div>
            </div>
            <p className="muted">Özel alan adı kaydı, doğrulaması ve etkinleştirmesi platform yöneticisi tarafından yapılır.</p>
              </section>

              {/* Account */}
              <section className="panel">
            <div className="panel-header">
              <div className="panel-header-text"><h2>Hesabınız</h2></div>
            </div>
            <dl style={{ display: "grid", gap: ".5rem", margin: 0 }}>
              <div style={{ display: "flex", justifyContent: "space-between", fontSize: ".875rem", paddingBottom: ".5rem", borderBottom: "1px solid var(--line-2)" }}>
                <dt style={{ color: "var(--muted)" }}>Ad</dt>
                <dd style={{ margin: 0, fontWeight: 500 }}>{principal.name}</dd>
              </div>
              <div style={{ display: "flex", justifyContent: "space-between", fontSize: ".875rem", paddingBottom: ".5rem", borderBottom: "1px solid var(--line-2)" }}>
				<dt style={{ color: "var(--muted)" }}>E-posta</dt>
				<dd style={{ margin: 0, fontWeight: 500 }}>{principal.email}</dd>
              </div>
              <div style={{ display: "flex", justifyContent: "space-between", fontSize: ".875rem" }}>
                <dt style={{ color: "var(--muted)" }}>Rol</dt>
                <dd style={{ margin: 0, fontWeight: 500 }}>{principal.role}</dd>
              </div>
            </dl>
              </section>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
