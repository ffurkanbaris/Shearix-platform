"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { ApiError, apiClient } from "@/lib/api";

export type CustomerSession = { id?: string; name: string; email: string; must_change_password: boolean };
type Session = { state: "loading" | "authenticated" | "unauthenticated" | "error"; customer?: CustomerSession; refresh: () => Promise<void> };
const Context = createContext<Session | null>(null);
export function useCustomerSession() { const value = useContext(Context); if (!value) throw new Error("useCustomerSession must be used inside CustomerSessionProvider"); return value; }

export function CustomerSessionProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<Session["state"]>("loading"); const [customer, setCustomer] = useState<CustomerSession>();
  const refresh = useCallback(async () => {
    try { const current = await apiClient.get<CustomerSession>("/v1/public/customer/auth/me"); setCustomer(current); setState("authenticated"); }
    catch (error) { setCustomer(undefined); setState(error instanceof ApiError && error.status === 401 ? "unauthenticated" : "error"); }
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => { const expired = () => { setCustomer(undefined); setState("unauthenticated"); }; window.addEventListener("barber:customer-session-expired", expired); return () => window.removeEventListener("barber:customer-session-expired", expired); }, []);
  return <Context.Provider value={{ state, customer, refresh }}>{children}</Context.Provider>;
}
