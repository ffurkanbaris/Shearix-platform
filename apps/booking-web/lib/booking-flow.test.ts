import { describe, expect, it } from "vitest";
import { bookingFingerprint, eligibleBarbers, idempotencyForSelection, isBookingConfig, selectionAfterChange, statusMessage, validGuest } from "@/lib/booking-flow";
import { proxyRequestHeaders } from "@/lib/proxy";
import { blankSelection, type Barber, type BarberServiceAssignment, type TenantConfig } from "@/lib/types";

const config: TenantConfig = {
  tenant_id: "tenant", app_type: "booking", business_name: "Demo", business_timezone: "Europe/Istanbul", booking_interval_minutes: 15,
  settings: { tenant_id: "tenant", business_timezone: "Europe/Istanbul", booking_interval_minutes: 15, reminder_offsets_minutes: [], cancellation_policy: "allow_until_notice", cancellation_notice_minutes: 120, booking_horizon_days: 60, minimum_booking_notice_minutes: 60 },
};

describe("booking flow", () => {
  it("accepts only booking tenant bootstrap data", () => {
    expect(isBookingConfig(config)).toBe(true);
    expect(isBookingConfig({ ...config, app_type: "admin" })).toBe(false);
  });

  it("filters active barbers by the authoritative public branch assignments", () => {
    const barbers: Barber[] = [
      { id: "a", display_name: "A", bio: "", active: true, branch_ids: ["branch-a"] },
      { id: "b", display_name: "B", bio: "", active: true, branch_ids: ["branch-b"] },
      { id: "c", display_name: "C", bio: "", active: false, branch_ids: ["branch-a"] },
    ];
    const assignments: BarberServiceAssignment[] = [{ barber_id: "a", service_ids: ["service-a"] }];
    expect(eligibleBarbers(barbers, "branch-a", "service-a", assignments).map((barber) => barber.id)).toEqual(["a"]);
    expect(eligibleBarbers(barbers, "branch-a", "service-b", assignments)).toEqual([]);
  });

  it("invalidates a selected slot when a service, branch, barber, or date changes", () => {
    const selected = { ...blankSelection, serviceID: "service-a", branchID: "branch-a", barberID: "barber-a", date: "2026-08-20", startAt: "2026-08-20T09:00:00Z" };
    const barbers: Barber[] = [{ id: "barber-a", display_name: "A", bio: "", active: true, branch_ids: ["branch-a"] }];
    const assignments: BarberServiceAssignment[] = [{ barber_id: "barber-a", service_ids: ["service-a"] }];
    expect(selectionAfterChange(selected, { serviceID: "service-b" }, barbers, assignments)).toMatchObject({ barberID: "", startAt: "" });
    expect(selectionAfterChange(selected, { branchID: "branch-b" })).toMatchObject({ barberID: "", startAt: "" });
    expect(selectionAfterChange(selected, { date: "2026-08-21" })).toMatchObject({ date: "2026-08-21", startAt: "" });
    expect(selectionAfterChange(selected, { barberID: "barber-b" })).toMatchObject({ date: "", startAt: "" });
  });

	 it("normalizes the guest email and changes the logical idempotency request only for material changes", () => {
	const selected = { ...blankSelection, branchID: "branch", barberID: "barber", serviceID: "service", startAt: "start", customerName: "Ada", customerEmail: "ADA@Example.com" };
    expect(validGuest(selected)).toBe(true);
	expect(bookingFingerprint(selected)).toBe(bookingFingerprint({ ...selected, customerEmail: "ada@example.com" }));
	 expect(bookingFingerprint(selected)).not.toBe(bookingFingerprint({ ...selected, startAt: "another-start" }));
  });

  it("reuses the idempotency key after an uncertain retry and rotates it for a semantic change", () => {
    const selected = { ...blankSelection, branchID: "branch", barberID: "barber", serviceID: "service", startAt: "start", customerName: "Ada", customerEmail: "ada@example.com" };
    let sequence = 0;
    const createKey = () => `key-${++sequence}`;
    const first = idempotencyForSelection(undefined, selected, createKey);
    const retry = idempotencyForSelection(first, { ...selected, customerEmail: "ADA@example.com" }, createKey);
    const changed = idempotencyForSelection(retry, { ...selected, startAt: "another-start" }, createKey);
    expect(retry.key).toBe(first.key);
    expect(changed.key).not.toBe(first.key);
  });

  it("maps a slot conflict to a refreshable booking conflict", () => {
    expect(statusMessage(409)).toBe("conflict");
    expect(statusMessage(404)).toBe("unavailable");
  });
});

describe("public API proxy", () => {
  it("keeps only safe browser headers and strips tenant/internal authority", () => {
    const headers = new Headers({ host: "booking.localhost:3001", accept: "application/json", "idempotency-key": "key", "x-tenant-id": "spoof", "x-app-type": "admin", "x-internal-token": "secret", "x-platform-admin-token": "secret" });
    const forwarded = proxyRequestHeaders(headers);
    expect(forwarded.get("host")).toBe("booking.localhost:3001");
    expect(forwarded.get("idempotency-key")).toBe("key");
    expect(forwarded.get("x-tenant-id")).toBeNull();
    expect(forwarded.get("x-app-type")).toBeNull();
    expect(forwarded.get("x-internal-token")).toBeNull();
    expect(forwarded.get("x-platform-admin-token")).toBeNull();
  });
});
