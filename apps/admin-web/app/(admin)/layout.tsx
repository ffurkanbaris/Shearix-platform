import { AuthGate } from "@/components/auth-gate";
import { AdminShell } from "@/components/shell";

export default function AdminLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <AuthGate><AdminShell>{children}</AdminShell></AuthGate>;
}
