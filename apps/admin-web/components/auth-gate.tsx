"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { ApiError, apiClient } from "@/lib/api";
import type { Principal } from "@/lib/types";

type SessionState = { principal: Principal; refresh: () => Promise<void>; expire: () => void };
const SessionContext = createContext<SessionState | null>(null);

export function useSession(): SessionState {
  const context = useContext(SessionContext);
  if (!context) throw new Error("useSession must be used inside AuthGate");
  return context;
}

export function AuthGate({ children }: Readonly<{ children: React.ReactNode }>) {
  const router = useRouter();
  const pathname = usePathname();
  const [principal, setPrincipal] = useState<Principal>();
  const [failure, setFailure] = useState<string>();

  const expire = useCallback(() => {
    setPrincipal(undefined);
    router.replace(`/login?next=${encodeURIComponent(pathname)}`);
  }, [pathname, router]);

  const refresh = useCallback(async () => {
    try {
      setFailure(undefined);
      const current = await apiClient.get<Principal>("/v1/admin/auth/me");
      setPrincipal(current);
	  if (current.must_change_password && pathname !== "/settings") router.replace("/settings");
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 401) {
        expire();
        return;
      }
      setFailure("We could not verify your session. Check the connection and try again.");
    }
  }, [expire, pathname, router]);

  useEffect(() => {
    void refresh();
    // Session bootstrap is intentionally once per application-shell mount.
    // Backend authorization remains authoritative for every subsequent API call.
  }, [refresh]);

  useEffect(() => {
    window.addEventListener("barber:session-expired", expire);
    return () => window.removeEventListener("barber:session-expired", expire);
  }, [expire]);

  if (failure) {
    return <main className="centered-state"><h1>Connection problem</h1><p>{failure}</p><button className="button primary" onClick={() => void refresh()}>Try again</button></main>;
  }
  if (!principal) return <main className="centered-state" aria-live="polite">Loading your workspace…</main>;
  return <SessionContext.Provider value={{ principal, refresh, expire }}>{children}</SessionContext.Provider>;
}
