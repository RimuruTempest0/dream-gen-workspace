import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";

// In-app update against a local mock: the QA instance is told to trust a fresh
// key and read update.json from the mock. One click checks, downloads and
// verifies (SHA-256 and contents); the QA instance refuses the restart itself,
// so nothing is ever installed.
let browser: Browser, page: Page, server: http.Server, base: string;
let manifest = Buffer.alloc(0),
  pkg = Buffer.alloc(0);
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
    [name, args] as const,
  ) as Promise<T>;
}

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    req.resume();
    if (req.url?.endsWith("/update.json")) {
      res.setHeader("content-type", "application/json");
      res.end(manifest);
      return;
    }
    res.setHeader("content-type", "application/zip");
    res.end(pkg);
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  base = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  await go("ResetDesktopQA");
});
test.afterAll(async () => {
  await go("StartTestUpdate", "").catch(() => {});
  await page?.goto("http://wails.localhost/#/");
  await browser?.close();
  server.closeAllConnections();
  await new Promise<void>((r) => server.close(() => r()));
});

test("软件更新：未设置地址时提示完整包；点一次下载并校验新版本", async () => {
  await page.goto("http://wails.localhost/#/settings");
  const panel = page.locator("#update");
  await expect(panel.getByText("此版本没有设置在线更新")).toBeVisible();

  const files = await go<{ manifest: string; package: string }>(
    "StartTestUpdate",
    base,
  );
  manifest = Buffer.from(files.manifest, "base64");
  pkg = Buffer.from(files.package, "base64");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect(
    panel.getByRole("switch", { name: "自动检查更新" }),
  ).toBeChecked();
  await panel.getByRole("button", { name: "检查更新" }).click();
  await expect(panel.getByText("新版本 9.9.9")).toBeVisible();
  // The rail line opens the same offer: version, notes and the one button.
  const badge = page.locator(".update-badge");
  await expect(badge).toHaveText(/有新版本 9\.9\.9/);
  await badge.click();
  const dialog = page.getByRole("dialog", { name: "软件更新" });
  await expect(dialog.getByText("QA 测试更新")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "立即更新" })).toBeVisible();
  // Scan only once the dialog has finished fading in.
  await page.waitForFunction(() => document.getAnimations().length === 0);
  const dialogA11y = await new AxeBuilder({ page })
    .include(".ant-modal")
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(dialogA11y.violations).toEqual([]);
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  // The rail stays open, so the rail line still reads in full.
  await expect(page.locator(".update-badge")).toContainText("有新版本 9.9.9");
  // The button beside the wordmark collapses it to icons and back.
  const toggle = page.getByRole("button", { name: "收起侧栏" });
  await toggle.click();
  await expect(page.locator(".rail")).toHaveCSS("width", "64px");
  await expect(page.locator(".update-badge .rail-label")).toBeHidden();
  await page.getByRole("button", { name: "展开侧栏" }).click();
  await expect(page.locator(".update-badge .rail-label")).toBeVisible();

  // One click: no second confirmation. It downloads, verifies and goes on to
  // the restart, which the QA instance refuses.
  const install = panel.getByRole("button", { name: "立即更新" });
  // Scan only after the dialog has gone and colour transitions have finished.
  await page.waitForFunction(() => document.getAnimations().length === 0);
  const a11y = await new AxeBuilder({ page })
    .include("#update")
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);
  await install.click();
  await expect(panel.getByText("测试环境不安装更新")).toBeVisible({
    timeout: 15000,
  });
  await expect(page.getByRole("dialog")).toHaveCount(0);

  // A tampered package is refused after download.
  pkg = Buffer.concat([pkg, Buffer.from("x")]);
  await go("StartTestUpdate", base).then((f: any) => {
    manifest = Buffer.from(f.manifest, "base64");
    pkg = Buffer.concat([Buffer.from(f.package, "base64"), Buffer.from("x")]);
  });
  await panel.getByRole("button", { name: "检查更新" }).click();
  await panel.getByRole("button", { name: "立即更新" }).click();
  await expect(panel.getByText(/大小不符|校验失败/).first()).toBeVisible({
    timeout: 15000,
  });
});
