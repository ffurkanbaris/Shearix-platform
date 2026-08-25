import { createHmac, scryptSync, timingSafeEqual } from "node:crypto";

export const cookieSecure = process.env.PLATFORM_COOKIE_SECURE === "true";
export const cookieName = cookieSecure ? "__Host-platform_admin_session" : "platform_admin_session";
export const cookieOptions = { httpOnly: true, secure: cookieSecure, sameSite: "strict" as const, path: "/" };
type Session = { email: string; exp: number };
const encode = (value: string) => Buffer.from(value).toString("base64url");

export function credentialsValid(email: string, password: string) {
  const expectedEmail = process.env.PLATFORM_ADMIN_EMAIL ?? "";
  const [saltHex, expectedHex] = (process.env.PLATFORM_ADMIN_PASSWORD_SCRYPT ?? "").split(":");
  const emailBytes = Buffer.from(email);
  const expectedEmailBytes = Buffer.from(expectedEmail);
  if (!expectedEmail || !saltHex || !expectedHex || emailBytes.length !== expectedEmailBytes.length) return false;
  const actual = scryptSync(password, Buffer.from(saltHex, "hex"), 32);
  const expected = Buffer.from(expectedHex, "hex");
  return expected.length === actual.length && timingSafeEqual(emailBytes, expectedEmailBytes) && timingSafeEqual(actual, expected);
}

export function createSession(email: string, now = Date.now()) {
  const payload = encode(JSON.stringify({ email, exp: now + 8 * 60 * 60 * 1000 } satisfies Session));
  return `${payload}.${sign(payload)}`;
}

export function readSession(value?: string): Session | null {
  if (!value) return null;
  const [payload, signature] = value.split(".");
  if (!payload || !signature) return null;
  const expected = sign(payload);
  if (signature.length !== expected.length || !timingSafeEqual(Buffer.from(signature), Buffer.from(expected))) return null;
  try { const session = JSON.parse(Buffer.from(payload, "base64url").toString()) as Session; return session.exp > Date.now() && session.email ? session : null; } catch { return null; }
}

function sign(payload: string) {
  const secret = process.env.PLATFORM_SESSION_SECRET ?? "";
  if (secret.length < 32) throw new Error("PLATFORM_SESSION_SECRET must contain at least 32 characters");
  return createHmac("sha256", secret).update(payload).digest("base64url");
}
