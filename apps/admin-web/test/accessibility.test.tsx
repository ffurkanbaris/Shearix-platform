import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import LoginPage from "@/app/login/page";

const mocks = vi.hoisted(() => ({ post: vi.fn(), replace: vi.fn(), refresh: vi.fn() }));
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, apiClient: { ...original.apiClient, post: mocks.post } };
});
vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace: mocks.replace, refresh: mocks.refresh }),
  useSearchParams: () => new URLSearchParams(),
}));

describe("admin login accessibility", () => {
  beforeEach(() => vi.clearAllMocks());

  it("has associated labels, a named submit control, and an announced async error", async () => {
    mocks.post.mockRejectedValue(new Error("unavailable"));
    const user = userEvent.setup();
    render(<LoginPage />);
    await user.type(screen.getByRole("textbox", { name: "Email" }), "owner@example.test");
    await user.type(screen.getByLabelText("Password"), "long-enough-value");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(await screen.findByRole("alert")).toBeTruthy();
  });

  it("submits a normalized email-only authentication payload", async () => {
    mocks.post.mockResolvedValue({ must_change_password: false });
    const user = userEvent.setup();
    render(<LoginPage />);
    await user.type(screen.getByRole("textbox", { name: "Email" }), "OWNER@EXAMPLE.TEST");
    await user.type(screen.getByLabelText("Password"), "long-enough-value");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(mocks.post).toHaveBeenCalledWith("/v1/admin/auth/login", {
      email: "owner@example.test",
      password: "long-enough-value",
    });
    expect(mocks.post.mock.calls[0][1]).not.toHaveProperty("phone");
    expect(screen.queryByLabelText(/phone/i)).toBeNull();
  });

  it("relies on email input validation before authentication submission", async () => {
    const user = userEvent.setup();
    render(<LoginPage />);
    await user.type(screen.getByRole("textbox", { name: "Email" }), "not-an-email");
    await user.type(screen.getByLabelText("Password"), "long-enough-value");
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    expect(mocks.post).not.toHaveBeenCalled();
  });
});
