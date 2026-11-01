import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import http from "node:http";
import fs from "node:fs";
import path from "node:path";

// A signed test customer package: trial on the trial key, then activation
// switches to the customer's own keys. All upstreams are local mocks.
let browser: Browser, page: Page, server: http.Server, base: string;
const keysUsed: string[] = [];
const png = fs
  .readFileSync(path.resolve(import.meta.dirname, "fixture.png"))
  .toString("base64");
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
    [name, args] as const,
  ) as Promise<T>;
}

test.describe.configure({ mode: "serial" });
test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    req.resume();
    const key = (req.headers.authorization || "").replace("Bearer ", "");
    res.setHeader("content-type", "application/json");
    if (req.url?.endsWith("/models")) {
      res.end(JSON.stringify({ data: [{ id: "mock-image" }] }));
      return;
    }
    keysUsed.push(key);
    res.end(JSON.stringify({ data: [{ b64_json: png }] }));
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
  await new Promise<void>((r) => server.close(() => r()));
});

test("客户版：试用 2 张、用完拦截、激活后切换到专属线路", async () => {
  const customer = await go<{ softwareID: string; code: string }>(
    "StartTestCustomer",
    base,
    base,
    2,
  );
  await page.goto("http://wails.localhost/#/settings");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  const badge = page.locator(".license-badge");
  await expect(badge).toHaveText(/试用剩余 2 \/ 2 张/);
  const panel = page.locator("#license");
  await expect(panel).toContainText(customer.softwareID);
  await expect(panel.getByText("试用中")).toBeVisible();
  // Customers see lines, never addresses or keys, and cannot add or edit them.
  await expect(page.getByRole("button", { name: "添加连接" })).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "添加 LLM 供应商" }),
  ).toHaveCount(0);
  await expect(
    page.locator(".connection-row").filter({ hasText: "图片线路 1" }),
  ).toBeVisible();
  // Trial: the main lines only; the backups are neither listed nor usable.
  await expect(
    page.locator(".connection-row").filter({ hasText: "图片线路 2" }),
  ).toHaveCount(0);
  expect(
    (await go<{ managed?: string }[]>("ListConnections"))
      .map((c) => c.managed)
      .sort(),
  ).toEqual(["image-1", "llm-1"]);
  expect(
    ((await go("GetGatewaySettings")) as { backupIDs: string[] }).backupIDs,
  ).toEqual([]);
  await expect(page.getByText("由软件授权管理").first()).toBeVisible();
  await expect(page.getByText(base)).toHaveCount(0);

  const lines =
    await go<{ id: string; kind: string; managed?: string }[]>(
      "ListConnections",
    );
  const image = lines.find((c) => c.managed === "image-1")!;
  const submit = (count: number) =>
    go("Submit", {
      requestID: crypto.randomUUID(),
      connectionID: image.id,
      model: "mock-image",
      prompts: ["客户试用"],
      count,
      references: [],
      size: "auto",
      quality: "auto",
      projectID: "",
    });
  await expect(submit(3)).rejects.toThrow(/试用剩余 2 张/);
  await submit(2);
  await expect.poll(() => keysUsed.length, { timeout: 20000 }).toBe(2);
  expect(keysUsed).toEqual(["line-image-1", "line-image-1"]);
  await expect(badge).toHaveText(/试用剩余 0 \/ 2 张/, { timeout: 15000 });
  await expect(submit(1)).rejects.toThrow(/试用额度已用完/);
  await page.getByRole("link", { name: "生图工作台" }).click();
  await expect(page.getByText("试用额度已用完", { exact: true })).toBeVisible();

  await page
    .locator(".license-notice")
    .getByRole("link", { name: "输入激活码" })
    .click();
  const code = page.getByLabel("激活码", { exact: true });
  await code.fill("AAAAA-BBBBB-CCCCC-DDDDD-EEEEE");
  await page.getByRole("button", { name: "激活", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "激活码与本软件不匹配" }),
  ).toBeVisible();
  await code.fill(customer.code.toLowerCase());
  await page.getByRole("button", { name: "激活", exact: true }).click();
  await expect(panel.getByText("已激活", { exact: true })).toBeVisible();
  await expect(badge).toHaveText(/已激活/);
  // Activated: the same lines, now with the backup.
  await expect(
    page.locator(".connection-row").filter({ hasText: "图片线路 2" }),
  ).toBeVisible();
  expect(
    ((await go("GetGatewaySettings")) as { backupIDs: string[] }).backupIDs,
  ).toHaveLength(1);

  await submit(1);
  await expect.poll(() => keysUsed.length, { timeout: 20000 }).toBe(3);
  expect(keysUsed[2]).toBe("line-image-1");
  const status = await go<{ mode: string }>("LicenseStatus");
  expect(status.mode).toBe("active");

  await page.getByRole("link", { name: "连接与设置" }).click();
  const a11y = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);
  const cdp = await browser.contexts()[0].newCDPSession(page);
  await cdp.send("Emulation.setDeviceMetricsOverride", {
    width: 900,
    height: 740,
    deviceScaleFactor: 1,
    mobile: false,
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
  ).toBe(false);
  await page.screenshot({
    path: "../docs/customer-license-0.6.0.png",
    fullPage: true,
  });
  await cdp.send("Emulation.clearDeviceMetricsOverride");
  await cdp.detach();
});
