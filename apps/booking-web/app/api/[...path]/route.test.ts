import { afterEach, describe, expect, it, vi } from "vitest";
import type { NextRequest } from "next/server";
import { proxyGateway } from "../../../../shared/server-proxy";

describe("booking Next.js proxy route wiring", () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; vi.unstubAllEnvs(); });
  const context = (path: string[]) => ({ params: Promise.resolve({ path }) });

  it("wires GET query and cookies through the actual route", async () => {
    const upstream = vi.fn().mockResolvedValue(new Response("ok", { status: 200, headers: { "content-type": "text/plain" } }));
    const response = await proxyGateway(new Request("http://booking.localhost/api/v1/public/config?lang=tr", { headers: { cookie: "session=opaque", "x-tenant-id": "spoof", "x-app-type": "admin", "x-internal-token": "spoof" } }) as NextRequest, ["v1", "public", "config"], upstream);
    expect(response.status).toBe(200);
    const [url, init] = upstream.mock.calls[0] as [URL, RequestInit];
    expect(url.toString()).toBe("http://gateway-service:8080/api/v1/public/config?lang=tr");
    const headers = init.headers as Headers;
    expect(headers.get("cookie")).toBe("session=opaque");
    expect(headers.get("x-tenant-id")).toBeNull();
    expect(headers.get("x-app-type")).toBeNull();
    expect(headers.get("x-internal-token")).toBeNull();
  });

  it("wires POST body and idempotency key and preserves application responses", async () => {
    const headers = new Headers({ "content-type": "application/json" });
    headers.append("set-cookie", "one=1; HttpOnly; SameSite=Lax");
    headers.append("set-cookie", "two=2; HttpOnly; Secure");
    const upstream = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "idempotency_conflict" }), { status: 409, headers }));
    const response = await proxyGateway(new Request("http://booking.localhost/api/v1/public/appointments", { method: "POST", headers: { "content-type": "application/json", "idempotency-key": "stable-key" }, body: JSON.stringify({ start_at: "2030-01-01T10:00:00Z" }) }) as NextRequest, ["v1", "public", "appointments"], upstream);
    expect(response.status).toBe(409);
    expect(response.headers.getSetCookie()).toHaveLength(2);
    const [, init] = upstream.mock.calls[0] as [URL, RequestInit];
    expect((init.headers as Headers).get("idempotency-key")).toBe("stable-key");
    expect(new TextDecoder().decode(init.body as ArrayBuffer)).toContain("start_at");
  });

  it("maps actual-route upstream network and timeout failures", async () => {
    const unavailable = await proxyGateway(new Request("http://booking.localhost/api/v1/public/config") as NextRequest, ["v1", "public", "config"], vi.fn().mockRejectedValueOnce(new TypeError("network")));
    expect(unavailable.status).toBe(502);

    vi.stubEnv("GATEWAY_REQUEST_TIMEOUT_MS", "1");
    const hanging = vi.fn((...args: Parameters<typeof fetch>) => new Promise<Response>((_resolve, reject) => {
      (args[1]?.signal as AbortSignal).addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
    }));
    const timeout = await proxyGateway(new Request("http://booking.localhost/api/v1/public/config") as NextRequest, ["v1", "public", "config"], hanging);
    expect(timeout.status).toBe(504);
  });
});
