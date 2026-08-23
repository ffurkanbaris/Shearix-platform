// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CustomerSessionProvider, useCustomerSession } from "./session";
import { ApiError } from "@/lib/api";

const mocks = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, apiClient: { ...original.apiClient, get: mocks.get } };
});

function State() {
  const session = useCustomerSession();
  return <div><span>{session.state}</span><span>{session.customer?.must_change_password ? "password-change-required" : "no-password-gate"}</span></div>;
}

describe("booking customer session boundary", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(cleanup);

  it("deduplicates session bootstrap and preserves must_change_password", async () => {
    mocks.get.mockResolvedValue({ id: "customer", name: "Customer", email: "customer@example.test", must_change_password: true });
    render(<CustomerSessionProvider><State /></CustomerSessionProvider>);
    await screen.findByText("authenticated");
    expect(screen.getByText("password-change-required")).toBeTruthy();
    expect(mocks.get).toHaveBeenCalledTimes(1);
  });

  it("distinguishes 401 from recoverable backend errors", async () => {
    mocks.get.mockRejectedValueOnce(new ApiError(503));
    const first = render(<CustomerSessionProvider><State /></CustomerSessionProvider>);
    await screen.findByText("error");
    first.unmount();
    mocks.get.mockRejectedValueOnce(new ApiError(401));
    render(<CustomerSessionProvider><State /></CustomerSessionProvider>);
    await screen.findByText("unauthenticated");
  });

  it("expires a loaded session only through the centralized 401 event", async () => {
    mocks.get.mockResolvedValue({ id: "customer", name: "Customer", email: "customer@example.test", must_change_password: false });
    render(<CustomerSessionProvider><State /></CustomerSessionProvider>);
    await screen.findByText("authenticated");
    act(() => window.dispatchEvent(new Event("barber:customer-session-expired")));
    await waitFor(() => expect(screen.getByText("unauthenticated")).toBeTruthy());
  });
});
