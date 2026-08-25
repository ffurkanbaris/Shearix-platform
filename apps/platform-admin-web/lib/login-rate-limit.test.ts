import { scryptSync } from "node:crypto";
import { beforeEach, describe, expect, it } from "vitest";
import { handleLogin } from "./login-handler";
import { allowPlatformLogin, loginLimit, loginWindowMs, type LimitStore } from "./login-rate-limit";

class MemoryStore implements LimitStore {
  now = 0;
  private values = new Map<string, { count: number; expires: number }>();
  async allow(key: string, limit: number, windowMs: number) {
    const current = this.values.get(key);
    const value = !current || current.expires <= this.now ? { count: 0, expires: this.now + windowMs } : current;
    value.count += 1;
    this.values.set(key, value);
    return value.count <= limit;
  }
}

const loginRequest = (email: string, password: string) => new Request("http://platform.localhost/api/login", {
  method: "POST",
  headers: { "content-type": "application/json", "x-platform-client-ip": "192.0.2.10" },
  body: JSON.stringify({ email, password }),
});

describe("platform login rate limit", () => {
  beforeEach(() => {
    const salt = "00112233445566778899aabbccddeeff";
    process.env.PLATFORM_ADMIN_EMAIL = "ops@example.com";
    process.env.PLATFORM_ADMIN_PASSWORD_SCRYPT = `${salt}:${scryptSync("correct password", Buffer.from(salt, "hex"), 32).toString("hex")}`;
    process.env.PLATFORM_SESSION_SECRET = "a-secret-that-is-definitely-longer-than-thirty-two";
  });

  it("allows attempts within the fixed window and blocks the next attempt", async () => {
    const store = new MemoryStore();
    for (let attempt = 0; attempt < loginLimit; attempt += 1) {
      expect(await allowPlatformLogin("ops@example.com", "192.0.2.10", store)).toEqual({ allowed: true, unavailable: false });
    }
    expect(await allowPlatformLogin("ops@example.com", "192.0.2.10", store)).toEqual({ allowed: false, unavailable: false });
  });

  it("recovers after the Redis key TTL window", async () => {
    const store = new MemoryStore();
    for (let attempt = 0; attempt <= loginLimit; attempt += 1) await allowPlatformLogin("ops@example.com", "192.0.2.10", store);
    store.now += loginWindowMs;
    expect(await allowPlatformLogin("ops@example.com", "192.0.2.10", store)).toEqual({ allowed: true, unavailable: false });
  });

  it("fails closed when Redis is unavailable", async () => {
    const store: LimitStore = { allow: async () => { throw new Error("redis unavailable"); } };
    expect(await allowPlatformLogin("ops@example.com", "192.0.2.10", store)).toEqual({ allowed: false, unavailable: true });
  });

  it("rejects invalid credentials without echoing the account", async () => {
    const response = await handleLogin(loginRequest("missing@example.com", "wrong"), async () => ({ allowed: true, unavailable: false }));
    expect(response.status).toBe(401);
    expect(await response.text()).not.toContain("missing@example.com");
  });

  it("returns a generic 429 before credential evaluation when blocked", async () => {
    const response = await handleLogin(loginRequest("missing@example.com", "wrong"), async () => ({ allowed: false, unavailable: false }));
    expect(response.status).toBe(429);
    expect(response.headers.get("retry-after")).toBe("900");
    expect(await response.text()).not.toContain("missing@example.com");
  });
});
