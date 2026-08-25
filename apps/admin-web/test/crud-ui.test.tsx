import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import BranchesPage from "@/app/(admin)/branches/page";
import SchedulesPage from "@/app/(admin)/schedules/page";
import ServicesPage from "@/app/(admin)/services/page";
import StaffPage from "@/app/(admin)/staff/page";

const mocks = vi.hoisted(() => {
  class MockApiError extends Error {
    constructor(public readonly status: number, message: string) { super(message); }
  }
  return {
    MockApiError,
    get: vi.fn(),
    post: vi.fn(),
    patch: vi.fn(),
    put: vi.fn(),
    remove: vi.fn(),
    useSession: vi.fn(),
  };
});

vi.mock("@/lib/api", () => ({
  ApiError: mocks.MockApiError,
  apiClient: { get: mocks.get, post: mocks.post, patch: mocks.patch, put: mocks.put, delete: mocks.remove },
}));
vi.mock("@/components/auth-gate", () => ({ useSession: mocks.useSession }));

const owner = { identity_id: "owner", name: "Owner", email: "owner@example.com", phone: "+905550000001", must_change_password: false, role: "OWNER" as const, tenant_id: "tenant", session_id: "session" };
const receptionist = { ...owner, role: "RECEPTIONIST" as const };
const manager = { ...owner, role: "MANAGER" as const };
const barberPrincipal = { ...owner, role: "BARBER" as const };
const branch = { id: "branch-1", name: "Original Branch", address: "Old address", active: true };
const service = { id: "service-1", name: "Haircut", price: "250.00", currency: "TRY", duration_minutes: 30, buffer_before_minutes: 5, buffer_after_minutes: 10, active: true };

beforeEach(() => {
  vi.clearAllMocks();
  mocks.useSession.mockReturnValue({ principal: owner });
  mocks.post.mockResolvedValue(undefined);
  mocks.patch.mockResolvedValue(undefined);
  mocks.remove.mockResolvedValue(undefined);
});

describe("admin CRUD dialogs", () => {
  it("loads a branch into an edit dialog without populating the create form, and cancel preserves the list", async () => {
    mocks.get.mockResolvedValue([branch]);
    const user = userEvent.setup();
    render(<BranchesPage />);
    await screen.findByText("Original Branch");
    await user.click(screen.getByRole("button", { name: /yeni şube/i }));
    const createDialog = screen.getByRole("dialog");
    const createName = within(createDialog).getByRole("textbox", { name: "Ad" });
    expect((createName as HTMLInputElement).value).toBe("");
    await user.click(within(createDialog).getByRole("button", { name: "Vazgeç" }));

    await user.click(screen.getByRole("button", { name: "Düzenle" }));
    const dialog = screen.getByRole("dialog");
    expect((within(dialog).getByLabelText("Ad") as HTMLInputElement).value).toBe("Original Branch");
    await user.clear(within(dialog).getByLabelText("Ad"));
    await user.type(within(dialog).getByLabelText("Ad"), "Changed only in dialog");
    await user.click(within(dialog).getByRole("button", { name: "Vazgeç" }));

    await user.click(screen.getByRole("button", { name: /yeni şube/i }));
    expect((within(screen.getByRole("dialog")).getByRole("textbox", { name: "Ad" }) as HTMLInputElement).value).toBe("");
    expect(screen.getByText("Original Branch")).toBeTruthy();
    expect(mocks.patch).not.toHaveBeenCalled();
  });

  it("saves an edit, refreshes the list, and requires confirmation before deactivation", async () => {
    let branches = [branch];
    mocks.get.mockImplementation(async () => branches);
    mocks.patch.mockImplementation(async (_path: string, payload: { name: string; address: string; active: boolean }) => {
      branches = [{ ...branch, ...payload }];
    });
    const user = userEvent.setup();
    render(<BranchesPage />);
    await screen.findByText("Original Branch");

    await user.click(screen.getByRole("button", { name: "Düzenle" }));
    const dialog = screen.getByRole("dialog");
    const name = within(dialog).getByLabelText("Ad");
    await user.clear(name); await user.type(name, "Updated Branch");
    await user.click(within(dialog).getByRole("button", { name: "Değişiklikleri kaydet" }));
    await screen.findByText("Updated Branch");
    expect(mocks.patch).toHaveBeenCalledWith("/v1/admin/branches/branch-1", expect.objectContaining({ name: "Updated Branch" }));

    await user.click(screen.getByRole("button", { name: "Pasife al" }));
    expect(screen.getByRole("dialog").textContent).toContain("Şube pasife alınsın mı?");
    await user.click(screen.getByRole("button", { name: "Şubeyi pasife al" }));
    await waitFor(() => expect(mocks.patch).toHaveBeenLastCalledWith("/v1/admin/branches/branch-1", expect.objectContaining({ active: false })));
  });

  it("renders backend validation errors inside the dedicated service edit dialog", async () => {
    mocks.get.mockImplementation(async (path: string) => path === "/v1/admin/services" ? [service] : []);
    mocks.patch.mockRejectedValue(new mocks.MockApiError(400, "Price must be an exact decimal"));
    const user = userEvent.setup();
    render(<ServicesPage />);
    await screen.findByText("Haircut");
    await user.click(screen.getByRole("button", { name: "Düzenle" }));
    const dialog = screen.getByRole("dialog");
    expect((within(dialog).getByLabelText("Fiyat") as HTMLInputElement).value).toBe("250.00");
    await user.click(within(dialog).getByRole("button", { name: "Değişiklikleri kaydet" }));
    expect(await within(dialog).findByText("Price must be an exact decimal")).toBeTruthy();
  });

  it("hides lifecycle actions from a read-only role", async () => {
    mocks.useSession.mockReturnValue({ principal: receptionist });
    mocks.get.mockResolvedValue([branch]);
    render(<BranchesPage />);
    await screen.findByText("Original Branch");
    expect(screen.queryByRole("button", { name: "Düzenle" })).toBeNull();
    expect(screen.queryByRole("button", { name: /yeni şube/i })).toBeNull();
  });

  it("reflects all backend branch-write role capabilities", async () => {
	for (const [principal, writable] of [[owner, true], [manager, true], [barberPrincipal, false], [receptionist, false]] as const) {
	  mocks.useSession.mockReturnValue({ principal });
	  mocks.get.mockResolvedValue([branch]);
	  const view = render(<BranchesPage />);
	  await screen.findByText("Original Branch");
	  expect(Boolean(screen.queryByRole("button", { name: "Düzenle" }))).toBe(writable);
	  expect(Boolean(screen.queryByRole("button", { name: /yeni şube/i }))).toBe(writable);
	  view.unmount();
	}
  });

  it("opens staff role changes in a dedicated dialog", async () => {
    const member = { identity_id: "member-1", name: "Staff Member", phone: "+905550000002", role: "RECEPTIONIST" as const, status: "active" as const, created_at: "2030-01-01T00:00:00Z" };
    mocks.get.mockResolvedValue([owner, member]);
    const user = userEvent.setup();
    render(<StaffPage />);
    await screen.findByText("Staff Member");
    await user.click(screen.getByRole("button", { name: "Rolü düzenle" }));
    const dialog = screen.getByRole("dialog");
    expect((within(dialog).getByLabelText("Rol") as HTMLSelectElement).value).toBe("RECEPTIONIST");
    await user.click(within(dialog).getByRole("button", { name: "Vazgeç" }));
    expect(mocks.patch).not.toHaveBeenCalled();
  });

  it("uses a separate override editor and confirms schedule deletes", async () => {
    const barber = { id: "barber-1", display_name: "A Barber", bio: "", active: true, branch_ids: [] };
    const schedule = {
      working_hours: [],
      overrides: [{ id: "override-1", date: "2030-01-08", kind: "custom_hours" as const, intervals: [{ start: "10:00", end: "14:00" }] }],
      blocked_periods: [{ id: "block-1", start_at: "2030-01-08T10:00:00Z", end_at: "2030-01-08T11:00:00Z" }],
    };
    mocks.get.mockImplementation(async (path: string) => path === "/v1/admin/barbers" ? [barber] : schedule);
    const user = userEvent.setup();
    render(<SchedulesPage />);
    await screen.findByText("2030-01-08 · custom_hours");
    await user.selectOptions(screen.getByLabelText("Type"), "custom_hours");
    const createIntervals = screen.getByPlaceholderText("10:00-14:00, 15:00-18:00");
    await user.click(screen.getByRole("button", { name: "Düzenle" }));
    const dialog = screen.getByRole("dialog");
    expect((within(dialog).getByLabelText("Intervals") as HTMLInputElement).value).toBe("10:00-14:00");
    await user.click(within(dialog).getByRole("button", { name: "Vazgeç" }));
    expect((createIntervals as HTMLInputElement).value).toBe("");

    await user.click(screen.getAllByRole("button", { name: "Sil" })[0]);
    expect(screen.getByRole("dialog").textContent).toContain("Özel gün ayarı silinsin mi?");
    await user.click(screen.getByRole("button", { name: "Özel gün ayarını sil" }));
    await waitFor(() => expect(mocks.remove).toHaveBeenCalledWith("/v1/admin/barbers/barber-1/overrides/override-1"));
  });
});
