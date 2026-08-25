import type { Metadata } from "next";
import "./globals.css";
import { CustomerSessionProvider } from "@/components/session";

export const metadata: Metadata = {
  title: "Randevu Al",
  description: "Berber randevusu alın",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="tr"><body><CustomerSessionProvider>{children}</CustomerSessionProvider></body></html>;
}
