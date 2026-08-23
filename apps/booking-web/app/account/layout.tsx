import { AccountGate } from "@/components/account-gate";

export default function AccountLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <AccountGate>{children}</AccountGate>;
}
