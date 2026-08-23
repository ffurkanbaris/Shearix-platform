// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";

describe("customer API session expiry contract", () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; });

  it.each([403, 503])("does not expire a valid customer session for status %d", async (status) => {
    let expirations = 0;
    window.addEventListener("barber:customer-session-expired", () => { expirations++; }, { once: true });
    global.fetch = vi.fn().mockResolvedValue(new Response(null, { status }));
    await expect(api("/v1/public/customer/test")).rejects.toMatchObject({ status });
    expect(expirations).toBe(0);
  });

  it("expires the customer session for a genuine 401", async () => {
    let expirations = 0;
    window.addEventListener("barber:customer-session-expired", () => { expirations++; }, { once: true });
    global.fetch = vi.fn().mockResolvedValue(new Response(null, { status: 401 }));
    await expect(api("/v1/public/customer/test")).rejects.toMatchObject({ status: 401 });
    expect(expirations).toBe(1);
  });
});
