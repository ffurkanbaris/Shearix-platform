import type { TenantSettings } from "@/lib/types";

export type SettingsForm = {
  timezone: string;
  booking_interval_minutes: string;
  reminder_offsets_minutes: string;
  cancellation_policy: TenantSettings["cancellation_policy"];
  booking_horizon_days: string;
  minimum_booking_notice_minutes: string;
};

export type UpdateTenantSettings = {
  timezone: string;
  booking_interval_minutes: number;
  reminder_offsets_minutes: number[];
  cancellation_policy: TenantSettings["cancellation_policy"];
  booking_horizon_days: number;
  minimum_booking_notice_minutes: number;
};

export function settingsForm(settings: TenantSettings): SettingsForm {
  return {
    timezone: settings.business_timezone,
    booking_interval_minutes: String(settings.booking_interval_minutes),
    reminder_offsets_minutes: settings.reminder_offsets_minutes.join(", "),
    cancellation_policy: settings.cancellation_policy,
    booking_horizon_days: String(settings.booking_horizon_days),
    minimum_booking_notice_minutes: String(settings.minimum_booking_notice_minutes),
  };
}

function integer(value: string, label: string): number {
  const parsed = Number(value);
  if (!Number.isInteger(parsed)) throw new Error(`${label} must be a whole number.`);
  return parsed;
}

// This is intentionally only a helpful client-side guard. The tenant-service
// remains authoritative for every validation rule.
export function settingsPayload(form: SettingsForm): UpdateTenantSettings {
  const offsets = form.reminder_offsets_minutes.split(",").map((value) => value.trim()).filter(Boolean).map((value) => integer(value, "Reminder offsets"));
  if (!form.timezone.trim()) throw new Error("Timezone is required.");
  if (offsets.length === 0 || offsets.some((value) => value <= 0) || new Set(offsets).size !== offsets.length) {
    throw new Error("Reminder offsets must be positive unique minutes separated by commas.");
  }
  const interval = integer(form.booking_interval_minutes, "Booking interval");
  const horizon = integer(form.booking_horizon_days, "Booking horizon");
  const notice = integer(form.minimum_booking_notice_minutes, "Minimum booking notice");
  if (interval < 1 || interval > 120) throw new Error("Booking interval must be between 1 and 120 minutes.");
  if (horizon < 1 || horizon > 365) throw new Error("Booking horizon must be between 1 and 365 days.");
  if (notice < 0 || notice > 10080) throw new Error("Minimum booking notice must be between 0 and 10080 minutes.");
  return {
    timezone: form.timezone.trim(),
    booking_interval_minutes: interval,
    reminder_offsets_minutes: offsets,
    cancellation_policy: form.cancellation_policy,
    booking_horizon_days: horizon,
    minimum_booking_notice_minutes: notice,
  };
}
