"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { type RefObject, useEffect, useRef, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";
import type { TenantConfig } from "@/lib/types";
import { canManageMembers, canWrite, roleLabels } from "@/lib/types";
import { useSession } from "./auth-gate";

/* ─── Icons ─────────────────────────────────────────────────────────────── */
function IconGrid() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="1" y="1" width="6" height="6" rx="1.5" fill="currentColor" opacity=".7"/>
      <rect x="9" y="1" width="6" height="6" rx="1.5" fill="currentColor" opacity=".7"/>
      <rect x="1" y="9" width="6" height="6" rx="1.5" fill="currentColor" opacity=".7"/>
      <rect x="9" y="9" width="6" height="6" rx="1.5" fill="currentColor" opacity=".7"/>
    </svg>
  );
}
function IconCalendar() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <rect x="1.5" y="2.5" width="13" height="12" rx="1.5" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <path d="M1.5 6h13" stroke="currentColor" strokeWidth="1.2" opacity=".7"/>
      <path d="M5 1v3M11 1v3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" opacity=".7"/>
      <circle cx="5.5" cy="9.5" r="1" fill="currentColor" opacity=".7"/>
      <circle cx="8" cy="9.5" r="1" fill="currentColor" opacity=".7"/>
      <circle cx="10.5" cy="9.5" r="1" fill="currentColor" opacity=".7"/>
    </svg>
  );
}
function IconUsers() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="6" cy="4.5" r="2.5" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <path d="M1 13c0-2.76 2.24-5 5-5s5 2.24 5 5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" fill="none" opacity=".7"/>
      <path d="M12 7c1.1 0 2 .9 2 2s-.9 2-2 2" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" fill="none" opacity=".6"/>
      <path d="M14.5 13c0-1.38-.67-2.6-1.7-3.36" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" fill="none" opacity=".6"/>
    </svg>
  );
}
function IconMapPin() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M8 1.5C5.51 1.5 3.5 3.51 3.5 6c0 3.37 4.5 8.5 4.5 8.5s4.5-5.13 4.5-8.5c0-2.49-2.01-4.5-4.5-4.5Z" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <circle cx="8" cy="6" r="1.5" fill="currentColor" opacity=".7"/>
    </svg>
  );
}
function IconScissors() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="4" cy="4" r="2" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <circle cx="4" cy="12" r="2" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <line x1="5.7" y1="5.7" x2="12" y2="12" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" opacity=".7"/>
      <line x1="5.7" y1="10.3" x2="9" y2="7" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" opacity=".7"/>
      <line x1="10" y1="5" x2="13" y2="2" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" opacity=".7"/>
    </svg>
  );
}
function IconTag() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M8.5 1.5H14.5V7.5L8 14a1.5 1.5 0 0 1-2.12 0L1.5 9.62a1.5 1.5 0 0 1 0-2.12L8.5 1.5Z" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <circle cx="11.5" cy="4.5" r="1" fill="currentColor" opacity=".7"/>
    </svg>
  );
}
function IconClock() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="6.5" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <path d="M8 4.5V8l2.5 2.5" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" fill="none" opacity=".7"/>
    </svg>
  );
}
function IconSettings() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="8" cy="8" r="2" stroke="currentColor" strokeWidth="1.4" fill="none" opacity=".7"/>
      <path d="M8 1v1.5M8 13.5V15M15 8h-1.5M2.5 8H1M12.72 3.28l-1.06 1.06M4.34 11.66l-1.06 1.06M12.72 12.72l-1.06-1.06M4.34 4.34l-1.06-1.06" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" opacity=".7"/>
    </svg>
  );
}
function IconMenu() {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none" aria-hidden="true">
      <path d="M3 5h14M3 10h14M3 15h14" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round"/>
    </svg>
  );
}

/* ─── Nav item icons map ─────────────────────────────────────────────────── */
const navIcons: Record<string, React.ReactNode> = {
  "/dashboard":    <IconGrid />,
  "/appointments": <IconCalendar />,
  "/staff":        <IconUsers />,
  "/branches":     <IconMapPin />,
  "/barbers":      <IconScissors />,
  "/services":     <IconTag />,
  "/schedules":    <IconClock />,
  "/settings":     <IconSettings />,
};

type NavigationItem = { href: string; label: string; visible: boolean };

export function AppRail({ children, open, railRef }: Readonly<{ children: React.ReactNode; open: boolean; railRef: RefObject<HTMLElement | null> }>) {
  return <aside ref={railRef} className={`sidebar${open ? " open" : ""}`} aria-label="Ana gezinme">{children}</aside>;
}

export function AdminShell({ children }: Readonly<{ children: React.ReactNode }>) {
  const { principal } = useSession();
  const pathname = usePathname();
  const router = useRouter();
  const [config, setConfig] = useState<TenantConfig>();
  const [configError, setConfigError] = useState("");
  const [open, setOpen] = useState(false);
  const [logoutError, setLogoutError] = useState("");
  const sidebarRef = useRef<HTMLElement>(null);

  function loadConfig() {
    const controller = new AbortController();
    setConfigError("");
    void apiClient.get<TenantConfig>("/v1/public/config", controller.signal)
      .then(setConfig)
      .catch((cause) => setConfigError(cause instanceof ApiError ? cause.message : "İşletme bilgileri yüklenemedi."));
    return controller;
  }

  useEffect(() => {
    const controller = loadConfig();
    return () => controller.abort();
  }, []);

  // Close sidebar on outside click (mobile)
  useEffect(() => {
    if (!open) return;
    function handle(e: MouseEvent) {
      if (sidebarRef.current && !sidebarRef.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", handle);
    return () => document.removeEventListener("mousedown", handle);
  }, [open]);

  if (configError) return <main className="centered-state"><h1>İşletme bilgileri yüklenemedi</h1><p>{configError}</p><button className="button primary" onClick={() => loadConfig()}>Tekrar dene</button></main>;
  if (!config) return <main className="centered-state" aria-live="polite">İşletme bilgileri yükleniyor…</main>;

  const businessName = config.business_name ?? "İşletmeniz";
  const initials = businessName.slice(0, 2).toUpperCase();

  const items: NavigationItem[] = [
    { href: "/dashboard",    label: "Genel Bakış", visible: true },
    { href: "/appointments", label: "Randevular", visible: true },
    { href: "/staff",        label: "Personel", visible: canManageMembers(principal.role) },
    { href: "/branches",     label: "Şubeler", visible: canWrite(principal.role) },
    { href: "/barbers",      label: "Berberler", visible: canWrite(principal.role) },
    { href: "/services",     label: "Hizmetler", visible: canWrite(principal.role) },
    { href: "/schedules",    label: "Çalışma Saatleri", visible: true },
    { href: "/settings",     label: "Ayarlar", visible: true },
  ];
  const visibleItems = items.filter((item) => item.visible);
  const activeItem   = visibleItems.find((item) => pathname === item.href || pathname.startsWith(`${item.href}/`));

  async function logout() {
    setLogoutError("");
    try { await apiClient.post<void>("/v1/admin/auth/logout"); router.replace("/login"); router.refresh(); }
    catch (cause) { setLogoutError(cause instanceof ApiError ? cause.message : "Çıkış yapılamadı. Lütfen tekrar deneyin."); }
  }

  const roleLabel = roleLabels[principal.role];

  return (
    <div className="app-shell">
      {/* Mobile top bar */}
      <header className="mobile-header">
        <button className="icon-button" aria-label="Gezinmeyi aç veya kapat" aria-expanded={open} aria-controls="admin-navigation" onClick={() => setOpen((v) => !v)}>
          <IconMenu />
        </button>
        <span>{activeItem?.label ?? businessName}</span>
      </header>

      {/* Persistent application rail */}
      <AppRail railRef={sidebarRef} open={open}>
        {/* Brand */}
        <div className="sidebar-brand">
          <span className="sidebar-brand-mark" aria-hidden="true">{initials}</span>
          <div>
            <div className="sidebar-brand-name">{businessName}</div>
            <div className="sidebar-brand-sub">Yönetim paneli</div>
          </div>
        </div>

        {/* Nav */}
        <nav className="sidebar-nav" id="admin-navigation">
          {visibleItems.map((item) => {
            const isActive = pathname === item.href || pathname.startsWith(`${item.href}/`);
            return (
              <Link
                key={item.href}
                href={item.href}
                onClick={() => setOpen(false)}
                className={`nav-item${isActive ? " active" : ""}`}
                aria-current={isActive ? "page" : undefined}
              >
                {navIcons[item.href]}
                {item.label}
              </Link>
            );
          })}
        </nav>

        {/* Account */}
        <div className="sidebar-account">
          <div className="sidebar-account-inner">
            <span className="sidebar-avatar" aria-hidden="true">{principal.name.slice(0, 1).toUpperCase()}</span>
            <div className="sidebar-account-name">{principal.name}</div>
            <div className="sidebar-account-role">{roleLabel}</div>
            {logoutError && <p className="error" role="alert" style={{ gridColumn: "1 / -1" }}>{logoutError}</p>}
            <button className="sidebar-signout" onClick={() => void logout()} style={{ gridColumn: "2" }}>
              Çıkış yap
            </button>
          </div>
        </div>
      </AppRail>

      {/* Content */}
      <div className="content-wrapper">
        {/* Desktop top bar */}
        <div className="topbar" role="banner">
          <div className="topbar-left"><span className="topbar-title">{activeItem?.label ?? businessName}</span></div>
        </div>
        <main className="content">{children}</main>
      </div>
    </div>
  );
}
