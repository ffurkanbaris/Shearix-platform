"use client";

import { useEffect } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useCustomerSession } from "@/components/session";

export function AccountGate({ children }: Readonly<{ children: React.ReactNode }>) {
  const { state: sessionState, customer, refresh } = useCustomerSession();
  const pathname = usePathname();
  const router = useRouter();

  useEffect(() => {
    if (sessionState === "unauthenticated") router.replace("/login");
    if (sessionState === "authenticated" && customer?.must_change_password && pathname !== "/account/security") {
      router.replace("/account/security");
    }
  }, [customer?.must_change_password, pathname, router, sessionState]);

  if (sessionState === "loading") return <main className="account-page"><p aria-live="polite">Hesabınız yükleniyor…</p></main>;
  if (sessionState === "error") return <main className="account-page"><p className="error" role="alert">Oturumunuz kontrol edilemedi. Lütfen tekrar deneyin.</p><button type="button" onClick={() => void refresh()}>Tekrar dene</button></main>;
  if (sessionState === "unauthenticated") return <main className="account-page"><p aria-live="polite">Giriş sayfasına yönlendiriliyorsunuz…</p></main>;
  if (customer?.must_change_password && pathname !== "/account/security") return <main className="account-page"><p aria-live="polite">Şifre ayarlarına yönlendiriliyorsunuz…</p></main>;
  return <>{children}</>;
}
