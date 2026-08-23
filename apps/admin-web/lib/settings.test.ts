import { describe, expect, it } from "vitest";
import { settingsForm, settingsPayload } from "./settings";
import type { TenantSettings } from "./types";

const settings: TenantSettings = {
  tenant_id: "tenant",
  business_timezone: "Europe/Istanbul",
  booking_interval_minutes: 15,
  reminder_offsets_minutes: [1440, 120],
  cancellation_policy: "allow_until_notice",
  cancellation_notice_minutes: 120,
  booking_horizon_days: 60,
  minimum_booking_notice_minutes: 60,
};

describe("tenant settings form", () => {
  it("preserves persisted settings and builds an explicit patch", () => {
    const form = settingsForm(settings);
    expect(form.reminder_offsets_minutes).toBe("1440, 120");
    expect(settingsPayload(form)).toEqual({
      timezone: "Europe/Istanbul",
      booking_interval_minutes: 15,
      reminder_offsets_minutes: [1440, 120],
      cancellation_policy: "allow_until_notice",
      booking_horizon_days: 60,
      minimum_booking_notice_minutes: 60,
    });
  });

  it("rejects duplicate or non-positive reminder offsets before saving", () => {
    expect(() => settingsPayload({ ...settingsForm(settings), reminder_offsets_minutes: "120, 120" })).toThrow("positive unique");
    expect(() => settingsPayload({ ...settingsForm(settings), reminder_offsets_minutes: "0" })).toThrow("positive unique");
  });
});
