import { scryptSync } from "node:crypto";
import { beforeEach, describe, expect, it } from "vitest";
import { createSession, credentialsValid, readSession } from "./session";

describe("platform session", () => {
  beforeEach(() => {
    const salt = "00112233445566778899aabbccddeeff";
    process.env.PLATFORM_ADMIN_EMAIL = "ops@example.com";
    process.env.PLATFORM_ADMIN_PASSWORD_SCRYPT = `${salt}:${scryptSync("correct password", Buffer.from(salt, "hex"), 32).toString("hex")}`;
    process.env.PLATFORM_SESSION_SECRET = "a-secret-that-is-definitely-longer-than-thirty-two";
  });
  it("keeps platform credentials separate and signs the session", () => {
    expect(credentialsValid("ops@example.com", "correct password")).toBe(true);
    expect(credentialsValid("owner@example.com", "correct password")).toBe(false);
    expect(readSession(createSession("ops@example.com"))?.email).toBe("ops@example.com");
  });
  it("rejects a modified session", () => expect(readSession(createSession("ops@example.com") + "x")).toBeNull());
  it("rejects a same-length Unicode email without throwing", () => {
    expect(() => credentialsValid("öps@example.com", "correct password")).not.toThrow();
    expect(credentialsValid("öps@example.com", "correct password")).toBe(false);
  });
});
