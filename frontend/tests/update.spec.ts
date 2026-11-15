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
// key and read update.json from the mock. The update is checked and downloaded
// (SHA-256 and contents verified) but never installed.
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
  await new Promise<void>((r) => server.close(() => r()));
});

test("软件更新：未设置地址时提示完整包；检查、下载并校验新版本", async () => {
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
  await expect(page.locator(".update-badge")).toHaveText(/有新版本 9\.9\.9/);
  await panel.getByRole("button", { name: "下载更新" }).click();
  const install = panel.getByRole("button", { name: "重启并更新" });
  await expect(install).toBeVisible({ timeout: 15000 });
  // Scan only after the button's colour transition has finished.
  await expect(install).toHaveCSS(
    "background-color",
    /^rgb\((239, 232, 218|255, 255, 255)\)$/,
  );
  const a11y = await new AxeBuilder({ page })
    .include("#update")
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);

  // A tampered package is refused after download.
  pkg = Buffer.concat([pkg, Buffer.from("x")]);
  await go("StartTestUpdate", base).then((f: any) => {
    manifest = Buffer.from(f.manifest, "base64");
    pkg = Buffer.concat([Buffer.from(f.package, "base64"), Buffer.from("x")]);
  });
  await panel.getByRole("button", { name: "检查更新" }).click();
  await panel.getByRole("button", { name: "下载更新" }).click();
  await expect(panel.getByText(/大小不符|校验失败/).first()).toBeVisible({
    timeout: 15000,
  });
});
