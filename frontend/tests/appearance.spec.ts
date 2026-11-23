// 明 / 暗 (0.8.3): the mark in the top-left corner switches between light and
// dark, the choice is saved by the backend, the settings page can go back to
// following Windows, and the mark stays reachable with the rail collapsed.
import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import path from "node:path";
test.describe.configure({ mode: "serial" });

const root = path.resolve(import.meta.dirname, "../..");
const cdp = process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223";
let browser: Browser, page: Page, before: string;

async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ name, args }) => window.go!.main!.App![name](...args),
    { name, args },
  ) as Promise<T>;
}
const mode = () => page.evaluate(() => document.documentElement.dataset.mode);
const wall = () =>
  page.evaluate(() => getComputedStyle(document.body).backgroundColor);
// The backend's reading of the Windows app mode, as the page uses it.
const windowsMode = () => go<string>("SystemAppearance");

test.beforeAll(async () => {
  browser = await chromium.connectOverCDP(cdp);
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  before = await go<string>("Appearance");
  await go("SetAppearance", "system");
  await page.goto("http://wails.localhost/#/settings");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
});
test.afterAll(async () => {
  if (page) {
    await go("SetAppearance", before);
    await page.reload();
  }
  await browser?.close();
});

test("following Windows, the mark switches 明 / 暗 and the choice is saved", async () => {
  const system = await windowsMode();
  const other = system === "dark" ? "light" : "dark";
  await expect.poll(mode).toBe(system);
  const panel = page.locator(".appearance-panel");
  await expect(panel.getByRole("radio", { name: "跟随系统" })).toBeChecked();

  const mark = page.locator(".rail-mark");
  await expect(mark).toHaveAccessibleName(
    other === "light" ? "切换到明" : "切换到暗",
  );
  await mark.hover();
  await expect(page.getByRole("tooltip")).toContainText("当前跟随系统");
  await mark.click();
  await expect.poll(mode).toBe(other);
  await expect
    .poll(wall)
    .toBe(other === "light" ? "rgb(242, 237, 227)" : "rgb(22, 19, 15)");
  expect(await go<string>("Appearance")).toBe(other);
  await expect(
    panel.getByRole("radio", { name: other === "light" ? "明" : "暗" }),
  ).toBeChecked();
  await expect(mark).toHaveAccessibleName(
    other === "light" ? "切换到暗" : "切换到明",
  );
  await page.mouse.move(600, 400);
  await page.waitForTimeout(800);
  await page.screenshot({
    path: path.join(root, "docs", `appearance-${other}.png`),
  });

  // The choice survives a reload; the settings page goes back to Windows.
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect.poll(mode).toBe(other);
  await page
    .locator(".appearance-panel")
    .getByText("跟随系统", { exact: true })
    .click();
  await expect.poll(mode).toBe(system);
  expect(await go<string>("Appearance")).toBe("system");
});

test("both modes keep the light page readable and the mark works collapsed", async () => {
  await go("SetAppearance", "light");
  await page.goto("http://wails.localhost/#/");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect.poll(mode).toBe("light");
  await page.waitForTimeout(600);
  await page.screenshot({
    path: path.join(root, "docs", "appearance-home-light.png"),
  });

  const collapse = page.locator(".rail-toggle");
  await collapse.click();
  const mark = page.locator(".rail-mark");
  await expect(mark).toBeVisible();
  await mark.click();
  await expect.poll(mode).toBe("dark");
  await page.locator(".rail-toggle").click();
  await expect(page.locator(".rail-wordmark")).toBeVisible();
});
