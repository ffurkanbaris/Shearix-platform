import type { Barber, BarberServiceAssignment, BookingSelection, TenantConfig } from "@/lib/types";

export function eligibleBarbers(barbers: Barber[], branchID: string, serviceID = "", assignments: BarberServiceAssignment[] = []): Barber[] {
  const assignment = new Map(assignments.map((item) => [item.barber_id, new Set(item.service_ids)]));
  return barbers.filter((barber) => barber.active && (!branchID || barber.branch_ids?.includes(branchID)) && (!serviceID || assignment.get(barber.id)?.has(serviceID)));
}

export function selectionAfterChange(current: BookingSelection, change: Partial<BookingSelection>, barbers: Barber[] = [], assignments: BarberServiceAssignment[] = []): BookingSelection {
  const next = { ...current, ...change };
  if (change.serviceID !== undefined || change.branchID !== undefined || change.barberID !== undefined) {
    next.date = "";
    next.startAt = "";
  } else if (change.date !== undefined) next.startAt = "";
  if (change.branchID !== undefined) next.barberID = "";
  if (change.serviceID !== undefined && next.barberID && !eligibleBarbers(barbers, next.branchID, next.serviceID, assignments).some((barber) => barber.id === next.barberID)) next.barberID = "";
  return next;
}

export function bookingFingerprint(selection: BookingSelection): string {
  return [selection.branchID, selection.barberID, selection.serviceID, selection.startAt, selection.customerName.trim(), selection.customerEmail.trim().toLowerCase()].join("|");
}

export type BookingIdempotency = { fingerprint: string; key: string };
export function idempotencyForSelection(previous: BookingIdempotency | undefined, selection: BookingSelection, createKey: () => string = () => crypto.randomUUID()): BookingIdempotency {
  const fingerprint = bookingFingerprint(selection);
  return previous?.fingerprint === fingerprint ? previous : { fingerprint, key: createKey() };
}

export function normalizePhone(value: string): string {
  return value.trim().replace(/[\s().-]/g, "");
}

export function validGuest(selection: BookingSelection): boolean {
  return selection.customerName.trim().length > 0 && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(selection.customerEmail.trim().toLowerCase());
}

export function isBookingConfig(config: TenantConfig): boolean {
  return config.app_type === "booking" && Boolean(config.business_timezone);
}

export function statusMessage(status: number): "conflict" | "unavailable" | "error" {
  if (status === 409) return "conflict";
  if (status === 404) return "unavailable";
  return "error";
}
