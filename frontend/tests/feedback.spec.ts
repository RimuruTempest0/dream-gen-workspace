import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

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
  // The legend's " *" is aria-hidden: the group is named by the legend text alone.
  await expect(
    dialog.getByRole("group", { name: "整套判断", exact: true }),
  ).toBeVisible();
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

test("the package preview counts the images and packing shows image x of y", async () => {
  // A sample, two versions of one image, an uploaded product photo and a crop.
  const runID = await go<string>("SeedTestImageSuite");
  await reload();
  await page
    .locator(".agent-history-row")
    .filter({ hasText: "QA suite: worn front with images" })
    .first()
    .getByRole("button", { name: "打开记录", exact: true })
    .click();
  await page.getByRole("button", { name: "问题反馈", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByLabel("整套可用", { exact: true }).check();
  await dialog
    .getByRole("button", { name: "保存反馈包…", exact: true })
    .click();
  await expect(dialog.getByText("images/", { exact: true })).toBeVisible();
  await expect(dialog).toContainText("生成图 3 张");
  await expect(dialog).toContainText("参考图 2 张");
  await expect(dialog).toContainText("含顾客上传的商品原图 1 张");
  await expect(dialog).toContainText("长边约 1600 像素");

  // The QA build packs slowly; the package is saved without the file dialog.
  const target = path.join(
    os.tmpdir(),
    `feedback-qa-${Date.now()}`,
    "问题反馈.zip",
  );
  fs.mkdirSync(path.dirname(target), { recursive: true });
  await page.evaluate(
    ({ runID, target }) => {
      (window as any).__feedbackExport = window.go!.main!.App![
        "ExportFeedbackPackageToPath"
      ](runID, target);
    },
    { runID, target },
  );
  const status = dialog.getByRole("status");
  await expect(status).toContainText(/正在打包第 [1-5]\/5 张/);
  await page.evaluate(() => (window as any).__feedbackExport);
  await expect(status).toHaveCount(0);
  expect(fs.statSync(target).size).toBeGreaterThan(0);
  fs.rmSync(path.dirname(target), { recursive: true, force: true });
});

// 逐张核验 (#20): optional per-image verdicts, kept with the suite.
test("single images can be marked, failed and unknown cards and each version are listed, and the marks are kept", async () => {
  const runID = await go<string>("SeedTestSuite", false);
  await reload();
  await openSuite("打开记录");
  await page.getByRole("button", { name: "问题反馈", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("逐张核验（选填）")).toBeVisible();
  const image = (name: string) =>
    dialog.getByRole("region", { name, exact: true });
  // Failed, 结果未知 and paused cards are in the list, and both versions of the
  // edited image with the change asked for then.
  await expect(image("第 1 张")).toContainText("生成失败");
  await expect(image("第 2 张")).toContainText("结果未知");
  await expect(image("第 4 张 · 第 2 版")).toContainText("已暂停");
  await expect(image("第 4 张 · 第 2 版")).toContainText("QA: brighter cuff");
  await expect(image("第 4 张 · 第 2 版")).toContainText("当前版本，共 2 版");
  await expect(image("第 4 张 · 第 1 版")).toContainText("较早的版本，共 2 版");

  // Per-image marks are optional: the suite verdict is still required.
  await image("第 2 张").getByLabel("有问题", { exact: true }).check();
  await image("第 2 张").getByLabel("没出图或报错", { exact: true }).check();
  const note = "重新生成全部失败图片后还是结果未知";
  await image("第 2 张")
    .getByLabel("第 2 张的说明", { exact: true })
    .fill(note);
  await image("第 4 张 · 第 1 版").getByLabel("合格", { exact: true }).check();
  await dialog.getByRole("button", { name: "保存反馈", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText("请先选择整套判断");
  await dialog.getByLabel("部分可用", { exact: true }).check();
  await dialog.getByLabel("几张图太像", { exact: true }).check();
  await dialog.getByRole("button", { name: "保存反馈", exact: true }).click();
  await expect(dialog.getByText(/上次保存/)).toBeVisible();
  const saved = await go<{
    items: { taskID: string; planItemID: string; verdict: string }[];
  }>("GetSuiteFeedback", runID);
  expect(saved.items.map((v) => [v.planItemID, v.verdict])).toEqual([
    ["qa-2", "problem"],
    ["qa-4", "ok"],
  ]);
  await dialog.getByRole("button", { name: "关闭", exact: true }).click();

  // Closed and opened again: the marks are still there.
  await reload();
  await openSuite("打开记录");
  await page.getByRole("button", { name: "问题反馈", exact: true }).click();
  await expect(
    image("第 2 张").getByLabel("有问题", { exact: true }),
  ).toBeChecked();
  await expect(
    image("第 2 张").getByLabel("没出图或报错", { exact: true }),
  ).toBeChecked();
  await expect(
    image("第 2 张").getByLabel("第 2 张的说明", { exact: true }),
  ).toHaveValue(note);
  await expect(
    image("第 4 张 · 第 1 版").getByLabel("合格", { exact: true }),
  ).toBeChecked();
  await expect(
    image("第 4 张 · 第 2 版").getByLabel("合格", { exact: true }),
  ).not.toBeChecked();
  // A mark can be cleared again.
  await image("第 4 张 · 第 1 版")
    .getByRole("button", { name: "清除第 4 张 · 第 1 版的标注", exact: true })
    .click();
  await expect(
    image("第 4 张 · 第 1 版").getByLabel("合格", { exact: true }),
  ).not.toBeChecked();
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
    dialog.getByRole("group", {
      name: "Verdict on the whole set",
      exact: true,
    }),
  ).toBeVisible();
  await expect(
    dialog.getByLabel("Some images are too similar", { exact: true }),
  ).toBeVisible();
  await expect(dialog.getByText("Image by image (optional)")).toBeVisible();
  await expect(
    dialog.getByRole("region", { name: "Image 4 · version 2", exact: true }),
  ).toContainText("Change asked for then: QA: brighter cuff");
  await expect(dialog).not.toContainText("整套判断");
  await go("SetInterfaceLanguage", "ja");
  await reload();
  await openSuite("開く");
  await page.getByRole("button", { name: "問題を報告", exact: true }).click();
  await expect(
    page
      .getByRole("dialog")
      .getByRole("group", { name: "セット全体の判定", exact: true }),
  ).toBeVisible();
  await go("SetInterfaceLanguage", "zh-CN");
});
