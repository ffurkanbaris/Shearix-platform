import { afterEach, describe, expect, it, vi } from "vitest";
import type { NextRequest } from "next/server";
import { proxyGateway } from "../../../../shared/server-proxy";

describe("admin Next.js proxy route wiring", () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it("uses the shared hardened proxy from the actual catch-all route", async () => {
    const upstream = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "server" }), { status: 500, headers: { "content-type": "application/json" } }));
    const response = await proxyGateway(
      new Request("http://admin.localhost/api/v1/admin/appointments?status=pending", { headers: { host: "admin.localhost", cookie: "admin=opaque", "x-user-id": "spoof", "x-tenant-id": "spoof" } }) as NextRequest,
      ["v1", "admin", "appointments"],
      upstream,
    );
    expect(response.status).toBe(500);
    const [url, init] = upstream.mock.calls[0] as [URL, RequestInit];
    expect(url.toString()).toBe("http://gateway-service:8080/api/v1/admin/appointments?status=pending");
    expect((init.headers as Headers).get("cookie")).toBe("admin=opaque");
    expect((init.headers as Headers).get("host")).toBe("admin.localhost");
    expect((init.headers as Headers).get("x-user-id")).toBeNull();
    expect((init.headers as Headers).get("x-tenant-id")).toBeNull();
  });
});
