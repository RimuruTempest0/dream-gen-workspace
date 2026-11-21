import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";

// 问题反馈 on a 套图 (#16). The suites are seeded by the QA build (SeedTestSuite):
// nothing is generated and no line is called.

let browser: Browser, page: Page;
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ name, args }) => window.go!.main!.App![name](...args),
    { name, args },
  ) as Promise<T>;
}
async function reload() {
  await page.goto("http://wails.localhost/#/");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
}
// Opens the seeded suite from the history list.
async function openSuite(open: string) {
  const row = page
    .locator(".agent-history-row")
    .filter({ hasText: "QA suite: worn front" })
    .first();
  await row.getByRole("button", { name: open, exact: true }).click();
}

test.beforeAll(async () => {
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
});
test.beforeEach(async () => {
  await go("ResetDesktopQA");
  await reload();
});
test.afterAll(async () => {
  if (page) {
    await go("ResetDesktopQA").catch(() => {});
    await reload().catch(() => {});
  }
  await browser?.close();
});

test("the feedback button is on finished and stopped suites and opens no prompt by itself", async () => {
  for (const stopped of [false, true]) {
    await go("ResetDesktopQA");
    await go<string>("SeedTestSuite", stopped);
    await reload();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await openSuite("打开记录");
    await expect(
      page.getByRole("button", { name: "问题反馈", exact: true }),
    ).toBeVisible();
    // Opening a suite never pops the panel up.
    await expect(page.getByRole("dialog")).toHaveCount(0);
  }
});

test("the verdict is required, the package is listed before saving and the feedback is kept", async () => {
  const runID = await go<string>("SeedTestSuite", true);
  await reload();
  await openSuite("打开记录");
  await page.getByRole("button", { name: "问题反馈", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("整套判断", { exact: true })).toBeVisible();
  await expect(dialog.getByPlaceholder("我要的是……，实际是……")).toBeVisible();

  // Nothing chosen: cannot save.
  await dialog.getByRole("button", { name: "保存反馈", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("请先选择整套判断");
  // Not usable, but nothing said: still cannot save.
  await dialog.getByLabel("整套不可用", { exact: true }).check();
  await dialog.getByRole("button", { name: "保存反馈", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("至少选一个问题类型");
  expect(
    (await go<{ verdict: string }>("GetSuiteFeedback", runID)).verdict,
  ).toBe("");

  await dialog.getByLabel("几张图太像", { exact: true }).check();
  const note = "我要的是礼盒图，实际是上身图";
  await dialog.getByLabel("整套说明", { exact: true }).fill(note);
  await dialog
    .getByRole("button", { name: "保存反馈包…", exact: true })
    .click();
  // The package's contents are listed before it is saved.
  await expect(dialog.getByText("反馈包里有这些内容").first()).toBeVisible();
  await expect(dialog.getByText("反馈.md", { exact: true })).toBeVisible();
  await expect(dialog.getByText("run.json", { exact: true })).toBeVisible();
  await expect(dialog).toContainText("不含 API Key、线路地址和授权文件");
  await expect(
    dialog.getByRole("button", { name: "选择位置并保存", exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "返回修改", exact: true }).click();
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();

  // Kept with the suite: still there after the page loads again.
  await reload();
  await openSuite("打开记录");
  await page.getByRole("button", { name: "问题反馈", exact: true }).click();
  await expect(dialog.getByLabel("整套不可用", { exact: true })).toBeChecked();
  await expect(dialog.getByLabel("几张图太像", { exact: true })).toBeChecked();
  await expect(dialog.getByLabel("整套说明", { exact: true })).toHaveValue(
    note,
  );
});

test("the panel follows the display language", async () => {
  await go<string>("SeedTestSuite", false);
  await go("SetInterfaceLanguage", "en");
  await reload();
  await openSuite("Open");
  await page
    .getByRole("button", { name: "Report a problem", exact: true })
    .click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByText("Verdict on the whole set", { exact: true }),
  ).toBeVisible();
  await expect(
    dialog.getByLabel("Some images are too similar", { exact: true }),
  ).toBeVisible();
  await expect(dialog).not.toContainText("整套判断");
  await go("SetInterfaceLanguage", "ja");
  await reload();
  await openSuite("開く");
  await page.getByRole("button", { name: "問題を報告", exact: true }).click();
  await expect(
    page.getByRole("dialog").getByText("セット全体の判定", { exact: true }),
  ).toBeVisible();
  await go("SetInterfaceLanguage", "zh-CN");
});
