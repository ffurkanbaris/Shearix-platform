// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";

describe("admin API session expiry contract", () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it.each([403, 503])("does not expire a valid session for status %d", async (status) => {
    let expirations = 0;
    window.addEventListener("barber:session-expired", () => { expirations++; }, { once: true });
    global.fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "safe_error" }), { status, headers: { "content-type": "application/json" } }));
    await expect(api("/v1/admin/test")).rejects.toMatchObject({ status });
    expect(expirations).toBe(0);
  });

  it("expires the session for a genuine 401", async () => {
    let expirations = 0;
    window.addEventListener("barber:session-expired", () => { expirations++; }, { once: true });
    global.fetch = vi.fn().mockResolvedValue(new Response(null, { status: 401 }));
    await expect(api("/v1/admin/test")).rejects.toMatchObject({ status: 401 });
    expect(expirations).toBe(1);
  });
});
