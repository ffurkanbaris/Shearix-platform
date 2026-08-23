// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CustomerLogin from "@/app/login/page";
import Register from "@/app/register/page";
import ForgotPassword from "@/app/forgot-password/page";
import Security from "@/app/account/security/page";

const mocks = vi.hoisted(() => ({ post: vi.fn(), push: vi.fn(), replace: vi.fn(), refresh: vi.fn() }));
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, apiClient: { ...original.apiClient, post: mocks.post } };
});
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: mocks.push, replace: mocks.replace }) }));
vi.mock("@/components/session", () => ({ useCustomerSession: () => ({ refresh: mocks.refresh }) }));

describe("critical customer form accessibility", () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.post.mockRejectedValue(new Error("unavailable")); });
  afterEach(cleanup);

  it.each([
    ["login", <CustomerLogin />, ["Email", "Password"], "Sign in"],
    ["registration", <Register />, ["Name", "Email"], "Create account"],
    ["password reset", <ForgotPassword />, ["Email"], "Send new password"],
    ["password change", <Security />, ["Current password", "New password"], "Change password"],
  ] as const)("provides labels, named submit state, and announced errors for %s", async (_name, component, labels, submitName) => {
    const user = userEvent.setup();
    render(component);
    for (const label of labels) {
      const input = screen.getByLabelText(label);
      expect(input).toBeTruthy();
      await user.type(input, label.includes("Email") ? "user@example.test" : "long-enough-value");
    }
    await user.click(screen.getByRole("button", { name: submitName }));
    expect(await screen.findByRole("alert")).toBeTruthy();
  });
});
