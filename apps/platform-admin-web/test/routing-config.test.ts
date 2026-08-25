import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { isSameOrigin } from "../lib/origin";

describe("platform host boundaries", () => {
  it("accepts only the exact mutation origin and rejects malformed origins", () => {
    expect(isSameOrigin("http://platform.localhost", "platform.localhost")).toBe(true);
    expect(isSameOrigin("https://platform.shearx.app", "platform.shearx.app")).toBe(true);
    expect(isSameOrigin("https://admin.tenant.example", "platform.shearx.app")).toBe(false);
    expect(isSameOrigin("not a URL", "platform.localhost")).toBe(false);
  });

  it("routes the local platform hostname directly and tenant hosts through gateway", () => {
    const caddyfile = readFileSync(resolve(process.cwd(), "../../infrastructure/caddy/Caddyfile.local"), "utf8");
    expect(caddyfile).toMatch(/http:\/\/platform\.localhost\s*\{[\s\S]*reverse_proxy platform-admin-web:3000/);
    expect(caddyfile).toContain("header_up X-Platform-Client-IP {http.request.remote.host}");
    expect(caddyfile).toMatch(/http:\/\/\s*\{[\s\S]*reverse_proxy gateway-service:8080/);
  });

  it("keeps production platform routing outside the tenant catch-all", () => {
    const caddyfile = readFileSync(resolve(process.cwd(), "../../infrastructure/caddy/Caddyfile"), "utf8");
    const platformRoute = caddyfile.indexOf("{$PLATFORM_ADMIN_HOST}");
    const tenantCatchAll = caddyfile.indexOf("https:// {");
    expect(platformRoute).toBeGreaterThan(-1);
    expect(platformRoute).toBeLessThan(tenantCatchAll);
    expect(caddyfile.slice(platformRoute, tenantCatchAll)).toContain("reverse_proxy platform-admin-web:3000");
  });

  it("serves the Shearx apex at the edge without changing tenant routing", () => {
    const caddyfile = readFileSync(resolve(process.cwd(), "../../infrastructure/caddy/Caddyfile"), "utf8");
    const apexRoute = caddyfile.indexOf("{$SAAS_DOMAIN}");
    const tenantCatchAll = caddyfile.indexOf("https:// {");
    expect(apexRoute).toBeGreaterThan(-1);
    expect(apexRoute).toBeLessThan(tenantCatchAll);
    expect(caddyfile.slice(apexRoute, tenantCatchAll)).toContain("respond \"<!doctype html>");
    expect(caddyfile.slice(tenantCatchAll)).toContain("header_up Host {http.request.host}");
    expect(caddyfile.slice(tenantCatchAll)).toContain("reverse_proxy gateway-service:8080");
  });

  it("pins the intended production apex and platform host in the safe template", () => {
    const envExample = readFileSync(resolve(process.cwd(), "../../infrastructure/production/production.env.example"), "utf8");
    expect(envExample).toContain("SAAS_DOMAIN=shearx.app");
    expect(envExample).toContain("PLATFORM_ADMIN_HOST=platform.shearx.app");
    expect(envExample).toContain("PRODUCTION_DOMAINS=shearx.app,platform.shearx.app");
  });
});
