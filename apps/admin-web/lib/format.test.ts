import { describe, expect, it } from "vitest";
import { apiErrorMessage, currency, dateKeyInTimezone } from "./format";

describe("admin UI formatting", () => {
  it("uses the backend status to give an actionable error", () => {
    expect(apiErrorMessage(403)).toContain("yetkiniz");
    expect(apiErrorMessage(409)).toContain("çakışıyor");
  });

  it("keeps exact price strings readable without doing business arithmetic", () => {
    expect(currency("125.50", "TRY")).toContain("125");
  });

  it("groups appointments by the tenant business timezone", () => {
    expect(dateKeyInTimezone("2026-01-01T21:30:00Z", "Europe/Istanbul")).toBe("2026-01-02");
  });
});
