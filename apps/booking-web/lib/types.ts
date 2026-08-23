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
  branch_ids?: string[];
}

export interface BarberServiceAssignment {
  barber_id: string;
  service_ids: string[];
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
  status: "pending" | "confirmed" | "cancelled" | "completed" | "no_show";
  can_cancel?: boolean;
  cancellation_deadline?: string;
}

export type BookingSelection = {
  serviceID: string;
  branchID: string;
  barberID: string;
  date: string;
  startAt: string;
  customerName: string;
  customerEmail: string;
};

export const blankSelection: BookingSelection = {
  serviceID: "", branchID: "", barberID: "", date: "", startAt: "", customerName: "", customerEmail: "",
};
