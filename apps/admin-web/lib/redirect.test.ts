import { describe, expect, it } from "vitest";
import { safeNext } from "./redirect";

describe("safeNext", () => {
  it("accepts same-origin application paths", () => { expect(safeNext("/dashboard")).toBe("/dashboard"); expect(safeNext("/staff?branch=x")).toBe("/staff?branch=x"); expect(safeNext("/settings/profile")).toBe("/settings/profile"); });
  it("rejects open redirect forms", () => { for (const value of ["https://evil.example", "//evil.example", "///evil.example", "javascript:alert(1)", "data:text/html,x", "\\evil.example"]) expect(safeNext(value)).toBe("/dashboard"); });
});
