import { act, render, screen, waitFor } from "@testing-library/react";
import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AuthGate } from "@/components/auth-gate";
import { ApiError } from "@/lib/api";

const mocks = vi.hoisted(() => ({ get: vi.fn(), replace: vi.fn(), pathname: "/appointments", router: {} as { replace: ReturnType<typeof vi.fn> } }));
mocks.router.replace = mocks.replace;
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, apiClient: { ...original.apiClient, get: mocks.get } };
});
vi.mock("next/navigation", () => ({ useRouter: () => mocks.router, usePathname: () => mocks.pathname }));

const principal = { identity_id: "owner", tenant_id: "tenant", session_id: "session", role: "OWNER", name: "Owner", email: "owner@example.test", phone: "", must_change_password: false };

describe("admin session boundary", () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.get.mockResolvedValue(principal); });

  it("loads the session once and expires centrally on a later 401", async () => {
    render(<AuthGate><div>privileged workspace</div></AuthGate>);
    await screen.findByText("privileged workspace");
    expect(mocks.get).toHaveBeenCalledTimes(1);
    act(() => window.dispatchEvent(new Event("barber:session-expired")));
    await waitFor(() => expect(mocks.replace).toHaveBeenCalledTimes(1));
    expect(mocks.replace).toHaveBeenCalledWith("/login?next=%2Fappointments");
  });

  it("keeps infrastructure failure distinct from unauthenticated", async () => {
    mocks.get.mockRejectedValue(new ApiError(503));
    render(<AuthGate><div>privileged workspace</div></AuthGate>);
    await screen.findByRole("heading", { name: "Bağlantı sorunu" });
    expect(mocks.replace).not.toHaveBeenCalled();
  });

  it("redirects once on bootstrap 401 and preserves the password-change gate", async () => {
    mocks.get.mockRejectedValueOnce(new ApiError(401));
    const first = render(<AuthGate><div>workspace</div></AuthGate>);
    await waitFor(() => expect(mocks.replace).toHaveBeenCalledTimes(1));
    first.unmount();
    vi.clearAllMocks();
    mocks.get.mockResolvedValue({ ...principal, must_change_password: true });
    render(<AuthGate><div>workspace</div></AuthGate>);
    await screen.findByText("workspace");
    expect(mocks.replace).toHaveBeenCalledWith("/settings");
  });
});
