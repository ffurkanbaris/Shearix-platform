import { NextResponse } from "next/server";
import { allowPlatformLogin, requestAddress } from "./login-rate-limit";
import { cookieName, cookieOptions, createSession, credentialsValid } from "./session";

type LoginLimiter = typeof allowPlatformLogin;

export async function handleLogin(request: Request, limiter: LoginLimiter = allowPlatformLogin) {
  const body = await request.json().catch(() => ({})) as { email?: string; password?: string };
  const decision = await limiter(body.email ?? "", requestAddress(request));
  if (!decision.allowed) return NextResponse.json(
    { error: decision.unavailable ? "Giriş geçici olarak kullanılamıyor" : "Çok fazla giriş denemesi. Lütfen daha sonra tekrar deneyin." },
    { status: decision.unavailable ? 503 : 429, headers: decision.unavailable ? undefined : { "retry-after": "900" } },
  );
  if (!credentialsValid(body.email ?? "", body.password ?? "")) return NextResponse.json({ error: "Giriş yapılamadı" }, { status: 401 });
  const response = NextResponse.json({ email: body.email });
  response.cookies.set(cookieName, createSession(body.email!), { ...cookieOptions, maxAge: 28800 });
  return response;
}
