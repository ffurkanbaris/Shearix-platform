import type { Metadata } from "next";
import "./globals.css";
import { CustomerSessionProvider } from "@/components/session";

export const metadata: Metadata = {
  title: "Book an appointment",
  description: "Book a barber appointment",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="en"><body><CustomerSessionProvider>{children}</CustomerSessionProvider></body></html>;
}
