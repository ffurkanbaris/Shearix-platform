import { describe, expect, it } from "vitest";
import { apiErrorMessage, currency } from "./format";

describe("admin UI formatting", () => {
  it("uses the backend status to give an actionable error", () => {
    expect(apiErrorMessage(403)).toContain("permission");
    expect(apiErrorMessage(409)).toContain("conflicts");
  });

  it("keeps exact price strings readable without doing business arithmetic", () => {
    expect(currency("125.50", "TRY")).toContain("125");
  });
});
