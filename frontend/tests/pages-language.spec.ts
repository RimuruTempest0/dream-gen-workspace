// 界面语言 (0.8.0) on 专业模式, 生成记录 and 作品与素材: in English,
// Vietnamese, Japanese and Korean none of the pages' own text is left in
// Chinese (including dialogs, the batch bar and Ant Design's table and
// pagination text), and a narrow window has no horizontal scroll.
import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import http from "node:http";
import fs from "node:fs";
import path from "node:path";
test.describe.configure({ mode: "serial" });

const cdp = process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223";
const png = fs
  .readFileSync(path.resolve(import.meta.dirname, "fixture.png"))
  .toString("base64");
const messages = path.resolve(import.meta.dirname, "../src/i18n/messages");
type Tables = Record<string, Record<string, string>>;
const tables = ["common", "workbench", "tasks", "assets"].map(
  (f) =>
    JSON.parse(
      fs.readFileSync(path.join(messages, f + ".json"), "utf8"),
    ) as Tables,
);
// Chinese texts of these pages (without placeholders) that must not show up in
// another language. Short ones (单字) are skipped: Japanese shares them.
function chineseLeft(text: string, lang: string) {
  const left: string[] = [];
  for (const t of tables)
    for (const [key, zh] of Object.entries(t["zh-CN"])) {
      if (t[lang][key] === zh) continue;
      for (const part of zh.split(/\{\w+\}/))
        if (part.trim().length >= 3 && text.includes(part.trim()))
          left.push(`${key}: ${part.trim()}`);
    }
  return left;
}
const han = /\p{Script=Han}/u;

let browser: Browser, page: Page, server: http.Server, baseURL: string;
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ name, args }) => window.go!.main!.App![name](...args),
    { name, args },
  ) as Promise<T>;
}
async function open(hash: string) {
  await page.goto("http://wails.localhost/#" + hash);
  await page.waitForFunction(() => !!window.go?.main?.App);
}
// The page's own text and accessible names. Components of other areas (mood,
// concept and template bars, license notice) and backend messages (#7) are
// left out; user data in this test is ASCII.
async function pageText() {
  return page.evaluate(() => {
    const roots = [
      document.querySelector(".page"),
      ...document.querySelectorAll(".ant-modal-root, .ant-notification"),
    ].filter(Boolean) as Element[];
    const parts: string[] = [];
    for (const root of roots) {
      const copy = root.cloneNode(true) as Element;
      copy
        .querySelectorAll(
          ".concept-bar, .template-bar, .license-notice, .mood-bar, .error-text, .task-error-details p, .ant-select-selection-item",
        )
        .forEach((e) => e.remove());
      parts.push(copy.textContent || "");
      copy
        .querySelectorAll("[aria-label],[placeholder],[title],[alt]")
        .forEach((e) => {
          for (const a of ["aria-label", "placeholder", "title", "alt"])
            if (e.getAttribute(a)) parts.push(e.getAttribute(a)!);
        });
    }
    return parts.join("\n");
  });
}
async function expectTranslated(lang: string, where: string) {
  const text = await pageText();
  expect(chineseLeft(text, lang), where).toEqual([]);
  if (lang !== "ja")
    expect(
      text.split("\n").filter((l) => han.test(l)),
      where,
    ).toEqual([]);
}
async function noHorizontalScroll(where: string) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    where,
  ).toBe(true);
}

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    req.resume();
    req.on("end", () => {
      res.setHeader("content-type", "application/json");
      if (req.url?.endsWith("/models"))
        return res.end(JSON.stringify({ data: [{ id: "qa-image" }] }));
      res.end(JSON.stringify({ data: [{ b64_json: png }] }));
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  baseURL = `http://127.0.0.1:${(server.address() as { port: number }).port}/v1`;
  browser = await chromium.connectOverCDP(cdp);
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  await go("ResetDesktopQA");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
});
test.afterAll(async () => {
  // Later specs expect a Chinese instance.
  if (page) {
    await page.setViewportSize({ width: 1414, height: 1000 }).catch(() => {});
    await go("ResetDesktopQA").catch(() => {});
    await page.goto("http://wails.localhost/#/").catch(() => {});
    await page.reload().catch(() => {});
  }
  await browser?.close();
  await new Promise<void>((r) => server?.close(() => r()));
});

test("pro mode, history and works pages show no Chinese in en, vi, ja and ko", async () => {
  const c = await go<{ id: string }>("SaveConnection", {
    name: "QA image service",
    baseURL,
    model: "qa-image",
    key: "qa-key-123",
  });
  // Enough images for the history table to page.
  for (let i = 0; i < 2; i++)
    await go("Submit", {
      requestID: crypto.randomUUID(),
      connectionID: c.id,
      model: "qa-image",
      prompts: ["language page qa " + i],
      count: 6,
      references: [],
      size: "1024x1024",
      quality: "high",
      projectID: "",
    });
  await expect
    .poll(
      async () => {
        const r = await go<{ items: { status: string }[]; total: number }>(
          "ListTasks",
          "succeeded",
          0,
          20,
        );
        return r.total;
      },
      { timeout: 30000 },
    )
    .toBe(12);
  await go("SetDraft", {
    connectionID: c.id,
    model: "",
    prompts: ["line one", "line two"],
    count: 1,
    references: [],
    size: "1024x1024",
    quality: "high",
    projectID: "",
  });

  for (const lang of ["en", "vi", "ja", "ko"]) {
    await go("SetInterfaceLanguage", lang);
    await page.setViewportSize({ width: 1414, height: 1000 });

    // 专业模式 with batch prompts, recent results and the image viewer.
    await open("/pro");
    await page.reload();
    await expect
      .poll(() => page.evaluate(() => document.documentElement.lang))
      .toBe(lang);
    await expect(page.locator(".recent-batch").first()).toBeVisible();
    await expect(page.locator(".result-item img").first()).toBeVisible();
    await expectTranslated(lang, `${lang} pro`);
    await page.locator(".parameter-grid .ant-select").nth(2).click();
    await expect(page.locator(".ant-select-dropdown:visible")).toBeVisible();
    const sizes = await page
      .locator(".ant-select-dropdown:visible .ant-select-item-option")
      .allTextContents();
    expect(sizes.filter((s) => han.test(s) && lang !== "ja")).toEqual([]);
    expect(chineseLeft(sizes.join("\n"), lang)).toEqual([]);
    await page.keyboard.press("Escape");
    await page.locator(".result-item .image-open").first().click();
    await expect(page.locator(".image-viewer img")).toBeVisible();
    await expectTranslated(lang, `${lang} viewer`);
    await page.keyboard.press("Escape");
    await expect(page.locator(".image-viewer")).toHaveCount(0);
    await page.locator(".generate-footer button").click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expectTranslated(lang, `${lang} generate dialog`);
    await page.getByRole("dialog").locator(".ant-btn").first().click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.setViewportSize({ width: 800, height: 900 });
    await noHorizontalScroll(`${lang} pro`);

    // 生成记录: table, filter, pagination and the detail dialog.
    await page.setViewportSize({ width: 1414, height: 1000 });
    await open("/tasks");
    await expect(page.locator(".ant-pagination-prev")).toBeVisible();
    await expectTranslated(lang, `${lang} tasks`);
    await page.locator(".records-panel .panel-heading .ant-select").click();
    const states = await page
      .locator(".ant-select-dropdown:visible .ant-select-item-option")
      .allTextContents();
    expect(states.length).toBeGreaterThan(5);
    expect(chineseLeft(states.join("\n"), lang)).toEqual([]);
    await page.keyboard.press("Escape");
    await page
      .locator("tbody .ant-table-row")
      .first()
      .getByRole("button")
      .first()
      .click();
    await expect(page.locator(".ant-modal .details")).toBeVisible();
    await expectTranslated(lang, `${lang} task detail`);
    await page.keyboard.press("Escape");
    await expect(page.locator(".ant-modal .details")).toHaveCount(0);
    await page.setViewportSize({ width: 800, height: 900 });
    await noHorizontalScroll(`${lang} tasks`);

    // 作品与素材: cards, the batch bar and the image detail.
    await page.setViewportSize({ width: 1414, height: 1000 });
    await open("/assets");
    await expect(page.locator(".asset-card").first()).toBeVisible();
    await expectTranslated(lang, `${lang} assets`);
    await page.locator(".works-panel .panel-heading button").click();
    await expect(page.locator(".asset-batch-bar")).toBeVisible();
    await page.locator(".asset-pick").first().click();
    await expectTranslated(lang, `${lang} assets batch`);
    await page.locator(".asset-batch-bar button").last().click();
    await page.locator(".asset-card .image-button").first().click();
    await expect(page.locator(".ant-modal .image-frame img")).toBeVisible();
    await expectTranslated(lang, `${lang} asset detail`);
    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 800, height: 900 });
    await noHorizontalScroll(`${lang} assets`);
    await page.screenshot({
      path: path.resolve(
        import.meta.dirname,
        `../../docs/desktop-pages-${lang}.png`,
      ),
    });
  }
});
