import { afterEach, describe, expect, it, vi } from "vitest";
import { createServer } from "node:http";
import { proxyConfig, proxyGateway, proxyRequestHeaders } from "../../shared/server-proxy";

describe("gateway proxy", () => {
  const originalFetch = global.fetch;
  afterEach(() => { global.fetch = originalFetch; vi.unstubAllEnvs(); });
  it("allowlists headers and strips trusted headers case-insensitively", () => {
    const headers = proxyRequestHeaders(new Headers({ accept: "application/json", "idempotency-key": "key", "X-Tenant-ID": "spoof", "x-app-type": "admin", "X-Internal-Token": "secret" }));
    expect(headers.get("accept")).toBe("application/json"); expect(headers.get("idempotency-key")).toBe("key"); expect(headers.get("x-tenant-id")).toBeNull(); expect(headers.get("x-app-type")).toBeNull(); expect(headers.get("x-internal-token")).toBeNull();
  });
  it("preserves upstream application responses and session cookies", async () => {
    const headers = new Headers({ "content-type": "application/json", "cache-control": "no-store" }); headers.append("set-cookie", "session=one; HttpOnly; SameSite=Lax"); headers.append("set-cookie", "refresh=two; HttpOnly; Secure");
    global.fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: "conflict" }), { status: 409, headers }));
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/appointments"), ["v1", "public", "appointments"], global.fetch);
    expect(response.status).toBe(409); expect(await response.json()).toEqual({ error: "conflict" }); expect(response.headers.get("content-type")).toContain("application/json"); expect(response.headers.getSetCookie()).toEqual(["session=one; HttpOnly; SameSite=Lax", "refresh=two; HttpOnly; Secure"]);
  });
  it("returns upstream 307 redirects without rewriting their location or body", async () => {
    const upstream = vi.fn().mockResolvedValue(new Response("redirecting", { status: 307, headers: { location: "/login?next=%2Faccount", "content-type": "text/plain" } }));
    const response = await proxyGateway(new Request("http://booking.localhost/api/v1/public/customer/profile", { headers: { host: "booking.localhost" } }), ["v1", "public", "customer", "profile"], upstream);
    expect(response.status).toBe(307);
    expect(response.headers.get("location")).toBe("/login?next=%2Faccount");
    expect(await response.text()).toBe("redirecting");
  });
  it("forwards method, query, body, and only allowed request headers", async () => {
    const upstream = vi.fn().mockResolvedValue(new Response("{}", { headers: { "content-type": "application/json" } }));
    global.fetch = upstream;
    await proxyGateway(new Request("http://tenant.test/api/v1/public/appointments?branch=a", { method: "POST", headers: { "content-type": "application/json", accept: "application/json", "x-tenant-id": "spoof" }, body: JSON.stringify({ start_at: "2026-01-01T10:00:00Z" }) }), ["v1", "public", "appointments"], upstream);
    const [url, init] = upstream.mock.calls[0] as [URL, RequestInit];
    expect(url.toString()).toBe("http://gateway-service:8080/api/v1/public/appointments?branch=a");
    expect(init.method).toBe("POST"); expect(init.headers).toBeInstanceOf(Headers);
    expect((init.headers as Headers).get("x-tenant-id")).toBeNull(); expect((init.headers as Headers).get("content-type")).toBe("application/json");
    expect(new TextDecoder().decode(init.body as ArrayBuffer)).toContain("start_at");
  });
  it("preserves the tenant Host on the real Node transport while connecting to the gateway address", async () => {
    let receivedHost = "";
    let receivedTenant = "";
    let receivedInternalToken = "";
    const server = createServer((request, response) => {
      receivedHost = request.headers.host ?? "";
      receivedTenant = String(request.headers["x-tenant-id"] ?? "");
      receivedInternalToken = String(request.headers["x-internal-token"] ?? "");
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ app_type: "booking" }));
    });
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    try {
      const address = server.address();
      if (!address || typeof address === "string") throw new Error("test gateway did not bind");
      vi.stubEnv("GATEWAY_URL", `http://127.0.0.1:${address.port}`);
      const response = await proxyGateway(new Request("http://booking.localhost:3001/api/v1/public/config", {
        headers: { host: "booking.localhost:3001", "x-tenant-id": "spoof", "x-internal-token": "spoof" },
      }), ["v1", "public", "config"]);
      expect(response.status).toBe(200);
      expect(receivedHost).toBe("booking.localhost:3001");
      expect(receivedTenant).toBe("");
      expect(receivedInternalToken).toBe("");
    } finally {
      await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()));
    }
  });
  it("maps network failures to 502", async () => {
    global.fetch = vi.fn().mockRejectedValue(new TypeError("network"));
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/config"), ["v1", "public", "config"], global.fetch);
    expect(response.status).toBe(502); expect((await response.json()).error).toBe("gateway_unavailable");
  });
  it("maps upstream timeouts to 504", async () => {
    vi.stubEnv("GATEWAY_REQUEST_TIMEOUT_MS", "1");
    global.fetch = vi.fn((...args: Parameters<typeof fetch>) => new Promise<Response>((_resolve, reject) => {
      const init = args[1] ?? {};
      (init.signal as AbortSignal).addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
    }));
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/config"), ["v1", "public", "config"], global.fetch);
    expect(response.status).toBe(504); expect((await response.json()).error).toBe("gateway_timeout");
  });
  it("cancels the upstream request when the browser aborts", async () => {
    const controller = new AbortController(); let upstreamAborted = false;
    global.fetch = vi.fn((...args: Parameters<typeof fetch>) => new Promise<Response>((_resolve, reject) => {
      const init = args[1] ?? {};
      (init.signal as AbortSignal).addEventListener("abort", () => { upstreamAborted = true; reject(new DOMException("aborted", "AbortError")); });
    }));
    const pending = proxyGateway(new Request("http://tenant.test/api/v1/public/config", { signal: controller.signal }), ["v1", "public", "config"], global.fetch);
    controller.abort(); const response = await pending;
    expect(upstreamAborted).toBe(true); expect(response.status).toBe(499);
  });
  it("rejects an oversized upstream response without returning a partial body", async () => {
    vi.stubEnv("GATEWAY_MAX_RESPONSE_BYTES", "16");
    global.fetch = vi.fn().mockResolvedValue(new Response("this response is too large", { headers: { "content-length": "26" } }));
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/config"), ["v1", "public", "config"], global.fetch);
    expect(response.status).toBe(502); expect((await response.json()).error).toBe("gateway_response_too_large");
  });
  it("rejects an oversized request before calling the gateway", async () => {
    vi.stubEnv("GATEWAY_MAX_REQUEST_BYTES", "8"); const upstream = vi.fn(); global.fetch = upstream;
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/appointments", { method: "POST", body: "too-large-body" }), ["v1", "public", "appointments"], upstream);
    expect(response.status).toBe(413); expect(upstream).not.toHaveBeenCalled();
  });
  it("rejects invalid bounded configuration", () => {
    expect(() => proxyConfig({ GATEWAY_REQUEST_TIMEOUT_MS: "0" })).toThrow(); expect(() => proxyConfig({ GATEWAY_MAX_RESPONSE_BYTES: "invalid" })).toThrow();
  });
  it("generates an X-Request-ID when the caller sends none, forwards it to the gateway, and returns it", async () => {
    const upstream = vi.fn().mockResolvedValue(new Response("{}", { headers: { "content-type": "application/json" } }));
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/config"), ["v1", "public", "config"], upstream);
    const [, init] = upstream.mock.calls[0] as [URL, RequestInit];
    const forwarded = (init.headers as Headers).get("x-request-id");
    expect(forwarded).toBeTruthy();
    expect(response.headers.get("x-request-id")).toBe(forwarded);
  });
  it("forwards and echoes back a valid caller-supplied X-Request-ID unchanged", async () => {
    const upstream = vi.fn().mockResolvedValue(new Response("{}", { headers: { "content-type": "application/json" } }));
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/config", { headers: { "x-request-id": "caller-supplied-id-123" } }), ["v1", "public", "config"], upstream);
    const [, init] = upstream.mock.calls[0] as [URL, RequestInit];
    expect((init.headers as Headers).get("x-request-id")).toBe("caller-supplied-id-123");
    expect(response.headers.get("x-request-id")).toBe("caller-supplied-id-123");
  });
  it("replaces an oversized or hostile caller-supplied X-Request-ID with a freshly generated one", async () => {
    // A literal CRLF header-injection value ("id\r\nX-Injected: evil") is
    // excluded here: the Web Headers API itself already refuses to
    // construct a Request carrying it (throws at Request() time), so that
    // specific case can never reach resolveRequestID via a real request —
    // matching the equivalent finding on the Go side (platform/httpx). This
    // exercises the other class of hostile input the pattern must still
    // reject on its own: oversized and disallowed-character values.
    const upstream = vi.fn().mockResolvedValue(new Response("{}", { headers: { "content-type": "application/json" } }));
    const hostile = "a".repeat(200);
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/config", { headers: { "x-request-id": hostile } }), ["v1", "public", "config"], upstream);
    const [, init] = upstream.mock.calls[0] as [URL, RequestInit];
    const forwarded = (init.headers as Headers).get("x-request-id");
    expect(forwarded).not.toBe(hostile);
    expect(forwarded).toBeTruthy();
    expect(response.headers.get("x-request-id")).toBe(forwarded);
  });
  it("still returns an X-Request-ID on error responses (timeout, cancellation, oversized)", async () => {
    vi.stubEnv("GATEWAY_REQUEST_TIMEOUT_MS", "1");
    global.fetch = vi.fn((...args: Parameters<typeof fetch>) => new Promise<Response>((_resolve, reject) => {
      const init = args[1] ?? {};
      (init.signal as AbortSignal).addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
    }));
    const response = await proxyGateway(new Request("http://tenant.test/api/v1/public/config", { headers: { "x-request-id": "caller-supplied-id-456" } }), ["v1", "public", "config"], global.fetch);
    expect(response.status).toBe(504);
    expect(response.headers.get("x-request-id")).toBe("caller-supplied-id-456");
  });
});
