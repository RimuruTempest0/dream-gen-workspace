// 心境引导 and 概念台 in the five interface languages (0.8.0): the whole guide
// and the stage read in the chosen language, the options come from the
// backend's built-in translations, nothing is cut off in a narrow window, and
// both pass the accessibility scan.
import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import fs from "node:fs";
import path from "node:path";
test.describe.configure({ mode: "serial" });

const messages = path.resolve(import.meta.dirname, "../src/i18n/messages");
type Tables = Record<string, Record<string, string>>;
const read = (f: string) =>
  JSON.parse(fs.readFileSync(path.join(messages, f), "utf8")) as Tables;
const moodText = read("mood.json");
const conceptText = read("concept.json");

const cdp = process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223";
let browser: Browser, page: Page;

async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ name, args }) => window.go!.main!.App![name](...args),
    { name, args },
  ) as Promise<T>;
}
const exact = (v: string) =>
  new RegExp(v.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));

// Han characters must not appear in en/vi/ko; in Japanese, characters that only
// simplified Chinese uses must not.
const han = /\p{Script=Han}/u;
const simplifiedOnly = /[选读这让远时气么吗样换确认请图]/;
function leftoverChinese(code: string, text: string) {
  if (code === "zh-CN") return false;
  return code === "ja" ? simplifiedOnly.test(text) : han.test(text);
}

// Text cut off inside the drawer, or a page wider than the window.
async function clipped(scope: string, selectors: string) {
  return page.evaluate(
    ({ scope, selectors }) => {
      const root = document.querySelector(scope)!;
      const cut = [...root.querySelectorAll(selectors)]
        .filter((e) => e.scrollWidth > e.clientWidth + 0.5)
        .map((e) => e.textContent);
      const body = root.querySelector(".ant-drawer-body");
      if (body && body.scrollWidth > body.clientWidth + 0.5)
        cut.push("drawer body scrolls sideways");
      if (document.documentElement.scrollWidth > innerWidth)
        cut.push("page scrolls sideways");
      return cut;
    },
    { scope, selectors },
  );
}

async function scan(include: string) {
  await page.mouse.move(0, 0);
  await page.waitForTimeout(400);
  const a11y = await new AxeBuilder({ page })
    .include(include)
    .withTags(["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);
}

test.beforeAll(async () => {
  browser = await chromium.connectOverCDP(cdp);
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
});
test.afterAll(async () => {
  // Later specs expect a Chinese instance in a normal window.
  if (page) {
    await page.setViewportSize({ width: 1414, height: 1000 }).catch(() => {});
    await go("ResetDesktopQA").catch(() => {});
    await page.goto("http://wails.localhost/#/").catch(() => {});
    await page.reload().catch(() => {});
  }
  await browser?.close();
});

const languages = [
  { system: "zh-CN", code: "zh-CN", drizzle: "细雨" },
  { system: "en-US", code: "en", drizzle: "Drizzle" },
  { system: "vi-VN", code: "vi", drizzle: "Mưa phùn" },
  { system: "ja-JP", code: "ja", drizzle: "霧雨" },
  { system: "ko-KR", code: "ko", drizzle: "가랑비" },
];

for (const { system, code, drizzle } of languages) {
  test(`mood guide and concept stage in ${code}`, async () => {
    test.setTimeout(90000);
    const M = moodText[code];
    const C = conceptText[code];
    await go("ResetDesktopQAWithSystemLanguage", system);
    await go("SetGuideMode", true);
    await page.goto("http://wails.localhost/#/pro");
    await page.reload();
    await page.waitForFunction(() => !!window.go?.main?.App);
    expect(await go("InterfaceLanguage")).toBe(code);
    await page.setViewportSize({ width: 800, height: 900 });

    // ---- 心境引导 ----
    const entry = page.getByRole("button", {
      name: exact(M["mood.entry.hint"]),
    });
    await expect(entry).toBeEnabled();
    await entry.click();
    const dialog = page.getByRole("dialog", { name: M["mood.guide"] });
    await expect(dialog).toBeVisible();
    const question = dialog.locator(".mood-question");
    await expect(question).toHaveText(M["mood.question.weather"]);
    // The options come from the backend in this language.
    await expect(
      dialog.getByRole("button", { name: drizzle, exact: true }),
    ).toBeVisible();
    await expect(
      dialog.getByRole("navigation", { name: M["mood.guide.steps"] }),
    ).toContainText(M["mood.step.expectation"]);
    expect(
      await clipped(".mood-guide", ".mood-option-name, .mood-progress-step"),
    ).toEqual([]);
    expect(leftoverChinese(code, await dialog.innerText())).toBe(false);
    await scan(".mood-guide");

    await dialog.getByRole("button", { name: drizzle, exact: true }).click();
    for (const next of [
      "mood.question.distance",
      "mood.question.expectation",
    ] as const) {
      await expect(question).not.toHaveText(M["mood.question.weather"]);
      await dialog.locator(".mood-option").first().click();
      await expect(question).toHaveText(M[next]);
    }
    await dialog.locator(".mood-option").last().click();
    const sentence = dialog.locator(".mood-sentence");
    await expect(sentence).toBeVisible();
    const said = await sentence.innerText();
    expect(said).not.toMatch(/[{}]/);
    expect(said).toContain(
      code === "zh-CN" ? "下着细雨" : code === "en" ? "drizzle" : "",
    );
    expect(
      await clipped(
        ".mood-guide",
        ".mood-pick-step, .mood-pick-name, .mood-progress-step, .mood-footer button",
      ),
    ).toEqual([]);
    expect(leftoverChinese(code, await dialog.innerText())).toBe(false);
    await scan(".mood-guide");
    await dialog.getByRole("button", { name: M["mood.apply"] }).click();
    await expect(dialog).toBeHidden();
    const moodBar = page.locator(".mood-bar-set");
    await expect(moodBar).toContainText(drizzle);
    expect(leftoverChinese(code, await moodBar.innerText())).toBe(false);

    // ---- 概念台 (picked with a mood: one stance) ----
    await page
      .getByRole("button", { name: exact(C["concept.entry.hint"]) })
      .click();
    const stage = page.getByRole("dialog", { name: C["concept.stage"] });
    await expect(stage).toBeVisible();
    await expect(stage.locator(".concept-section-title").first()).toHaveText(
      C["concept.picks"],
    );
    await stage.locator(".concept-pick").first().click();
    await expect(stage.locator(".concept-summary-title")).not.toContainText(
      C["concept.placeholder.form"],
    );
    expect(
      await clipped(
        ".concept-stage",
        ".concept-card-name, .concept-card-en, .concept-pick strong, .concept-pick span, .concept-group-name, .concept-section-title, .ant-segmented-item-label, .concept-actions button",
      ),
    ).toEqual([]);
    expect(leftoverChinese(code, await stage.innerText())).toBe(false);
    await scan(".concept-stage");
    await stage.getByRole("button", { name: C["concept.use"] }).click();
    await expect(stage).toBeHidden();
    const conceptBar = page.locator(".concept-bar-set");
    await expect(conceptBar).toContainText(" × ");
    expect(leftoverChinese(code, await conceptBar.innerText())).toBe(false);
  });
}
