import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";

// The mass-market program is one program for two editions (docs/adr/0003):
// without customer.dat beside it, the open edition its user configures; with it,
// the hosted edition on the provider's lines. All upstreams are local mocks.
let browser: Browser, page: Page, server: http.Server, base: string;
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
    [name, args] as const,
  ) as Promise<T>;
}
async function openSettings() {
  await page.goto("http://wails.localhost/#/settings");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
}

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    req.resume();
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({ data: [{ id: "mock-image" }] }));
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  base = `http://127.0.0.1:${(server.address() as { port: number }).port}/v1`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
});
test.afterAll(async () => {
  // Leave the QA instance in developer mode for the next spec.
  await go("ResetDesktopQA").catch(() => {});
  await page?.reload();
  await browser?.close();
  // The app keeps connections to the mock relay open (the settings page runs
  // the free check on open), so close them instead of waiting for them.
  server.closeAllConnections();
  await new Promise<void>((r) => server.close(() => r()));
});

test("大众版：开源版导入授权文件成为托管版，删除授权文件后回到开源版", async () => {
  const hosted = await go<{
    softwareID: string;
    code: string;
    file: string;
    program: string;
  }>("StartTestHosted", base, base, 2);
  const panel = page.locator("#license");
  const own = page
    .locator(".connection-row")
    .filter({ hasText: "我自己的图片服务" });
  const line = page
    .locator(".connection-row")
    .filter({ hasText: "图片线路 1" });
  const add = page.getByRole("button", { name: "添加连接" });

  // Open edition: the user configures their own service; a licence file can be imported.
  await go("SaveConnection", {
    name: "我自己的图片服务",
    baseURL: base,
    model: "mock-image",
    key: "my-own-key",
  });
  await openSettings();
  await expect(panel.getByText("托管版授权")).toBeVisible();
  await expect(panel.getByText("未使用")).toBeVisible();
  await expect(
    panel.getByRole("button", { name: "导入授权文件" }),
  ).toBeEnabled();
  await expect(page.locator(".license-badge")).toHaveCount(0);
  await expect(add).toBeVisible();
  await expect(own).toBeVisible();
  // The rail fades in after a reload: scan once it has finished.
  await page.waitForFunction(() => document.getAnimations().length === 0);
  const openScan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(openScan.violations).toEqual([]);

  // The file picker is a system dialog: import through the same call the button makes.
  const status = await go<{ mode: string; hosted: boolean }>(
    "ImportCustomerFile",
    hosted.file,
  );
  expect(status).toMatchObject({ mode: "trial", hosted: true });
  await openSettings();
  await expect(panel).toContainText(hosted.softwareID);
  await expect(panel.getByText("试用中")).toBeVisible();
  await expect(page.locator(".license-badge")).toHaveText(/试用剩余 2 \/ 2 张/);
  // Hosted edition: the provider's lines only; the user's own service is kept
  // but not listed, and none can be added.
  await expect(line).toBeVisible();
  await expect(own).toHaveCount(0);
  await expect(add).toHaveCount(0);
  await expect(panel.getByText(/想改用自己的图片服务和 AI 模型/)).toBeVisible();

  await page.getByLabel("激活码", { exact: true }).fill(hosted.code);
  await page.getByRole("button", { name: "激活", exact: true }).click();
  await expect(panel.getByText("已激活", { exact: true })).toBeVisible();
  // The rail fades in after a reload: scan once it has finished.
  await page.waitForFunction(() => document.getAnimations().length === 0);
  const hostedScan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(hostedScan.violations).toEqual([]);
  await page.screenshot({
    path: "../docs/hosted-edition-0.7.34.png",
    fullPage: true,
  });

  // customer.dat deleted from the program folder: the open edition again, the
  // provider's lines gone, the user's own service back.
  await go("RemoveTestLicenceFile", hosted.program);
  await openSettings();
  await expect(panel.getByText("未使用")).toBeVisible();
  await expect(own).toBeVisible();
  await expect(line).toHaveCount(0);
  await expect(add).toBeVisible();
  expect(
    (await go<{ mode: string; canImport: boolean }>("LicenseStatus")).mode,
  ).toBe("developer");
});
