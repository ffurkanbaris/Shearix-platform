// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import CustomerLogin from "@/app/login/page";
import Register from "@/app/register/page";
import ForgotPassword from "@/app/forgot-password/page";
import { normalizeAuthEmail } from "../../shared/auth-contract";

const mocks = vi.hoisted(() => ({ post: vi.fn(), push: vi.fn(), refresh: vi.fn() }));
vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, apiClient: { ...original.apiClient, post: mocks.post } };
});
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: mocks.push }) }));
vi.mock("@/components/session", () => ({ useCustomerSession: () => ({ refresh: mocks.refresh }) }));

describe("email-only customer authentication contracts", () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.post.mockResolvedValue({ must_change_password: false }); mocks.refresh.mockResolvedValue(undefined); });
  afterEach(cleanup);

  it("normalizes without retaining a phone fallback", () => {
    expect(normalizeAuthEmail("  USER@Example.Test ")).toBe("user@example.test");
  });

  it("logs in with email and routes temporary-password accounts to password change", async () => {
    mocks.post.mockResolvedValue({ must_change_password: true });
    const user = userEvent.setup();
    render(<CustomerLogin />);
    await user.type(screen.getByLabelText("E-posta"), "USER@EXAMPLE.TEST");
    await user.type(screen.getByLabelText("Şifre"), "temporary-password");
    await user.click(screen.getByRole("button", { name: "Giriş yap" }));

    expect(mocks.post).toHaveBeenCalledWith("/v1/public/customer/auth/login", {
      email: "user@example.test",
      password: "temporary-password",
    });
    expect(mocks.post.mock.calls[0][1]).not.toHaveProperty("phone");
    expect(mocks.refresh).toHaveBeenCalledOnce();
    expect(mocks.push).toHaveBeenCalledWith("/account/security");
    expect(screen.queryByLabelText(/phone/i)).toBeNull();
  });

  it("registers name and email and shows credential-delivery copy without a password", async () => {
    const user = userEvent.setup();
    render(<Register />);
    await user.type(screen.getByLabelText("Ad soyad"), "  Ada Lovelace  ");
    await user.type(screen.getByLabelText("E-posta"), "ADA@EXAMPLE.TEST");
    await user.click(screen.getByRole("button", { name: "Hesap oluştur" }));

    expect(mocks.post).toHaveBeenCalledWith("/v1/public/customer/auth/register", {
      name: "Ada Lovelace",
      email: "ada@example.test",
    });
    expect(mocks.post.mock.calls[0][1]).not.toHaveProperty("phone");
    expect((await screen.findByRole("status")).textContent).toMatch(/e-posta adresinize gönderildi/i);
    expect(screen.queryByText("temporary-password")).toBeNull();
  });

  it("requests password reset using only normalized email", async () => {
    const user = userEvent.setup();
    render(<ForgotPassword />);
    await user.type(screen.getByLabelText("E-posta"), "RESET@EXAMPLE.TEST");
    await user.click(screen.getByRole("button", { name: "Yeni şifre gönder" }));

    expect(mocks.post).toHaveBeenCalledWith("/v1/public/customer/auth/forgot-password", {
      email: "reset@example.test",
    });
    expect(mocks.post.mock.calls[0][1]).not.toHaveProperty("phone");
    expect((await screen.findByRole("status")).textContent).toMatch(/geçici şifre.*e-posta/i);
  });

  it.each([
    ["login", <CustomerLogin />, ["E-posta", "Şifre"], "Giriş yap"],
    ["registration", <Register />, ["Ad soyad", "E-posta"], "Hesap oluştur"],
    ["reset", <ForgotPassword />, ["E-posta"], "Yeni şifre gönder"],
  ] as const)("rejects an invalid email in the %s form before API submission", async (_name, component, labels, button) => {
    const user = userEvent.setup();
    render(component);
    for (const label of labels) await user.type(screen.getByLabelText(label), label === "E-posta" ? "invalid" : "valid-value");
    await user.click(screen.getByRole("button", { name: button }));
    expect(mocks.post).not.toHaveBeenCalled();
  });
});
