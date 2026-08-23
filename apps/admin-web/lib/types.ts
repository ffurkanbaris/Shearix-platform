export type Role = "OWNER" | "MANAGER" | "BARBER" | "RECEPTIONIST";

export interface Principal {
  identity_id: string;
  name: string;
  email: string;
  phone: string;
  must_change_password: boolean;
  role: Role;
  tenant_id: string;
  session_id: string;
}

export interface TenantSettings {
  tenant_id: string;
  business_timezone: string;
  booking_interval_minutes: number;
  reminder_offsets_minutes: number[];
  cancellation_policy: "allow_until_notice" | "no_cancellation";
  cancellation_notice_minutes: number;
  booking_horizon_days: number;
  minimum_booking_notice_minutes: number;
}

export interface TenantConfig {
  tenant_id: string;
  app_type: "admin" | "booking";
  business_name?: string;
  business_timezone: string;
  booking_interval_minutes: number;
  settings: TenantSettings;
}

export interface Member {
  identity_id: string;
  name: string;
  email: string;
  phone: string;
  role: Role;
  status: "active" | "inactive";
  created_at: string;
}

export interface Branch {
  id: string;
  name: string;
  address: string;
  active: boolean;
}

export interface Barber {
  id: string;
  display_name: string;
  bio: string;
  active: boolean;
  identity_id?: string;
  branch_ids?: string[];
}

export interface CatalogService {
  id: string;
  name: string;
  duration_minutes: number;
  buffer_before_minutes: number;
  buffer_after_minutes: number;
  price: string;
  currency: string;
  active: boolean;
}

export interface Interval {
  start: string;
  end: string;
}

export interface WorkingDay {
  weekday: number;
  intervals: Interval[];
}

export interface ScheduleOverride {
  id: string;
  date: string;
  kind: "unavailable" | "vacation" | "custom_hours";
  intervals?: Interval[];
}

export interface BlockedPeriod {
  id: string;
  start_at: string;
  end_at: string;
}

export interface Schedule {
  working_hours: WorkingDay[];
  overrides: ScheduleOverride[];
  blocked_periods: BlockedPeriod[];
}

export interface AvailabilitySlot {
  start_at: string;
  end_at: string;
}

export interface Appointment {
  id: string;
  branch_id: string;
  barber_id: string;
  service_id: string;
  customer_name: string;
  customer_contact: string;
  start_at: string;
  end_at: string;
  occupied_start_at: string;
  occupied_end_at: string;
  status: "pending" | "confirmed" | "cancelled" | "completed" | "no_show";
}

export const staffRoles: Role[] = ["MANAGER", "BARBER", "RECEPTIONIST"];

export function canWrite(role: Role): boolean {
  return role === "OWNER" || role === "MANAGER";
}

export function canManageMembers(role: Role): boolean {
  return role === "OWNER";
}

export function canManageTenantSettings(role: Role): boolean {
  return role === "OWNER" || role === "MANAGER";
}
