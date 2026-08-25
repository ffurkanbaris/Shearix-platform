import { expect, test, type Page } from "@playwright/test";
const tenant = { id: "11111111-2222-4333-8444-555555555555", name: "Çok Uzun Felsefi Berber İşletmesi — Şehrin Sessiz ve Düşünceli Köşesi", status: "active", created_at: "2026-08-25T00:00:00Z" };
async function mockPlatform(page: Page) {
  await page.route("**/api/platform/**", async route => {
    const url = new URL(route.request().url()); let body: unknown = {};
    if (url.pathname.endsWith("/tenants")) body = [tenant];
    else if (url.pathname.endsWith(`/tenants/${tenant.id}`)) body = tenant;
    else if (url.pathname.endsWith("/domains")) body = [{ id: "domain-1", hostname: "rezervasyon-cok-uzun-alt-alan-adi.ornek-isletme.example.com", domain_type: "booking", verified: true, active: true }];
    else if (url.pathname.endsWith("/owners")) body = [{ identity_id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", email: "cok.uzun.platform.sahibi.eposta.adresi@example-business-domain.com", identity_status: "active", membership_status: "active", must_change_password: false, delivery_status: "delivered" }];
    else if (url.pathname.includes("/audit")) body = [];
    else if (url.pathname.endsWith("/health")) body = { "notification-service-with-a-long-name": { ready: true, failure_indicators: { "notification_delivery_terminal_failures_total": 0 } } };
    else if (url.pathname.endsWith("/suspend")) body = { ...tenant, status: "suspended" };
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
}
async function expectContained(page: Page) {
  const metrics = await page.evaluate(() => ({ documentWidth: document.documentElement.scrollWidth, viewportWidth: document.documentElement.clientWidth }));
  expect(metrics.documentWidth).toBeLessThanOrEqual(metrics.viewportWidth);
  for (const element of await page.locator(".card, .top, .action-panel").all()) { const box = await element.boundingBox(); if (box) { expect(box.x).toBeGreaterThanOrEqual(-0.5); expect(box.x + box.width).toBeLessThanOrEqual(metrics.viewportWidth + 0.5); } }
}
test("long platform content remains contained across viewports", async ({ page }, testInfo) => {
  await mockPlatform(page); await page.goto("/"); await expect(page.getByText(tenant.name)).toBeVisible(); await expectContained(page);
  const table = await page.locator(".table-wrap").evaluate(element => ({ client: element.clientWidth, scroll: element.scrollWidth, overflow: getComputedStyle(element).overflowX }));
  expect(table.scroll).toBeGreaterThanOrEqual(table.client); expect(table.overflow).toBe("auto");
  await page.goto(`/tenants/${tenant.id}`); await expect(page.getByText(/rezervasyon-cok-uzun-alt-alan-adi/)).toBeVisible(); await expect(page.getByText(/cok\.uzun\.platform\.sahibi/)).toBeVisible(); await expectContained(page);
  await page.getByRole("button", { name: "Askıya al", exact: true }).click(); const panel = page.getByRole("alertdialog"); await expect(panel).toBeVisible(); await expectContained(page);
  if (testInfo.project.name === "mobile") { const panelBox = await panel.boundingBox(); for (const button of await panel.getByRole("button").all()) { const box = await button.boundingBox(); expect(box && panelBox && box.x >= panelBox.x && box.x + box.width <= panelBox.x + panelBox.width).toBeTruthy(); } }
});
