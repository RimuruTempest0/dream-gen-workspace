import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";

// The update notes follow the interface language: the QA manifest carries
// Chinese and English notes; a Chinese interface shows the Chinese ones, the
// other four languages the English ones (a mock serves update.json, nothing is
// installed).
let browser: Browser, page: Page, server: http.Server, base: string;
let manifest = Buffer.alloc(0),
  pkg = Buffer.alloc(0);
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
    [name, args] as const,
  ) as Promise<T>;
}
const english = "QA test update: only checking, downloading and verifying.";
const chinese = "QA 测试更新：只验证检查、下载和校验。";

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
  await go("SetInterfaceLanguage", "zh-CN").catch(() => {});
  await page?.goto("http://wails.localhost/#/");
  await page?.reload().catch(() => {});
  await browser?.close();
  server.closeAllConnections();
  await new Promise<void>((r) => server.close(() => r()));
});

async function openIn(lang: string) {
  await go("SetInterfaceLanguage", lang);
  await page.goto("http://wails.localhost/#/settings");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect(page.locator("html")).toHaveAttribute("lang", lang);
}

test("更新说明：英文界面一键更新显示英文说明，中文界面显示中文说明", async () => {
  const files = await go<{ manifest: string; package: string }>(
    "StartTestUpdate",
    base,
  );
  manifest = Buffer.from(files.manifest, "base64");
  pkg = Buffer.from(files.package, "base64");
  await openIn("en");
  await go("CheckForUpdate");

  // The one-click offer in the settings panel and behind the rail line.
  const panelNotes = page.locator("#update .update-notes");
  await expect(panelNotes).toHaveText(english);
  await expect(panelNotes).toHaveAttribute("lang", "en");
  await page.locator(".update-badge").click();
  const dialogNotes = page.locator(".ant-modal .update-notes");
  await expect(dialogNotes).toHaveText(english);
  await expect(page.locator(".ant-modal")).not.toContainText(chinese);
  // The zoom-in can start after the text is there; check the settled dialog.
  await expect(page.locator(".ant-modal")).not.toHaveClass(/ant-zoom/);
  await page.waitForFunction(() => document.getAnimations().length === 0);
  const a11y = await new AxeBuilder({ page })
    .include(".ant-modal")
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);
  await page.keyboard.press("Escape");
  await expect(page.locator(".ant-modal-wrap:visible")).toHaveCount(0);

  // Another non-Chinese language reads the English notes as well, and back in
  // Chinese the Chinese ones, without checking again.
  await openIn("ja");
  await expect(panelNotes).toHaveText(english);
  await openIn("zh-CN");
  await expect(panelNotes).toHaveText(chinese);
  await expect(panelNotes).toHaveAttribute("lang", "zh-CN");
});
