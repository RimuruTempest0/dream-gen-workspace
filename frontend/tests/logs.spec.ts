import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";

let browser: Browser, page: Page, server: http.Server, baseURL: string;
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ name, args }) => window.go!.main!.App![name](...args),
    { name, args },
  ) as Promise<T>;
}
async function queryModels(id: string) {
  await go("TestConnection", {
    id,
    kind: "llm",
    baseURL,
    key: "unsaved-log-key-789",
    authMode: "api-key",
  });
}
test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    res.writeHead(401, { "content-type": "application/json" });
    res.end(
      JSON.stringify({ error: { message: "unsaved-log-key-789 denied" } }),
    );
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  baseURL = `http://127.0.0.1:${(server.address() as { port: number }).port}/anthropic`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  await go("ResetDesktopQA");
  await page.goto("http://wails.localhost/#/");
});
test.afterAll(async () => {
  if (page) await page.goto("http://wails.localhost/#/");
  await browser?.close();
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

test("logs display real errors, redact keys, filter, refresh and show details", async () => {
  await queryModels("desktop-log-one");
  await page.getByRole("link", { name: "日志", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "日志", exact: true }),
  ).toBeVisible();
  await expect(page.locator("tbody").getByText(/HTTP 401/)).toBeVisible();
  await expect(page.locator("body")).not.toContainText("unsaved-log-key-789");
  await page
    .getByLabel("日志级别", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await page.getByText("警告", { exact: true }).last().click();
  await page
    .getByLabel("日志来源", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await page.getByText("连接验证", { exact: true }).last().click();
  await expect(page.getByRole("status")).toHaveText("共 1 条符合条件的日志");
  await page
    .getByLabel("关键词或关联 ID", { exact: true })
    .fill("desktop-log-one");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("共 1 条符合条件的日志");
  await page.getByRole("button", { name: /查看日志 .* 详情/ }).click();
  await expect(page.getByRole("dialog")).toContainText("desktop-log-one");
  await expect(page.getByRole("dialog")).toContainText("已隐藏");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "关闭", exact: true })
    .click();
  await page
    .getByLabel("关键词或关联 ID", { exact: true })
    .fill("没有这样的日志");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(
    page.getByText("暂无符合条件的日志，可调整筛选条件或稍后刷新。"),
  ).toBeVisible();
  await page.getByLabel("关键词或关联 ID", { exact: true }).fill("");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await page.getByRole("switch", { name: "自动刷新", exact: true }).click();
  await queryModels("desktop-log-two");
  await page.getByRole("button", { name: "刷新日志", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("共 2 条符合条件的日志");
  await expect(page.locator(".ant-notification-notice")).toHaveCount(0, {
    timeout: 8000,
  });
  const audit = await new AxeBuilder({ page }).analyze();
  expect(audit.violations).toEqual([]);
  await page.setViewportSize({ width: 800, height: 900 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 1414, height: 1100 });
  await page.screenshot({ path: "../docs/logs-0.2.2.png", fullPage: true });
});

test("logs paginate and filtering returns to the first page", async () => {
  for (let i = 0; i < 51; i++) await queryModels("paging-log-" + i);
  await page.getByRole("button", { name: "刷新日志", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("共 53 条符合条件的日志");
  await page.locator(".ant-pagination-item-2").click();
  await expect(page.locator("tbody .ant-table-row")).toHaveCount(3);
  await page
    .getByLabel("关键词或关联 ID", { exact: true })
    .fill("desktop-log-one");
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await expect(page.locator("tbody .ant-table-row")).toHaveCount(1);
  await expect(page.getByRole("status")).toHaveText("共 1 条符合条件的日志");
});
