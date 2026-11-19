// 界面语言 (0.8.0): first start follows the (simulated) Windows language,
// the switch in the rail and on the settings page applies at once, Ant
// Design's own text, dates and fonts follow, and the choice survives a real
// restart of the program.
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
import { spawn } from "node:child_process";
test.describe.configure({ mode: "serial" });

const root = path.resolve(import.meta.dirname, "../..");
const exe = path.join(root, ".tools", "desktop-qa", "ImageWorkbench.exe");
const pidFile = path.join(root, ".tools", "desktop-qa.pid");
const cdp = process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223";
let browser: Browser, page: Page, server: http.Server, baseURL: string;

async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    async ({ name, args }) => window.go!.main!.App![name](...args),
    { name, args },
  ) as Promise<T>;
}
async function attach() {
  browser = await chromium.connectOverCDP(cdp);
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
}
// A fresh data directory, first started on a Windows with this UI language.
async function firstStart(systemLanguage: string) {
  await go("ResetDesktopQAWithSystemLanguage", systemLanguage);
  // A new hash alone does not reload; the page must load again to start over.
  await page.goto("http://wails.localhost/#/");
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
}
const lang = () => page.evaluate(() => document.documentElement.lang);
const prevPage = () =>
  page.locator(".ant-pagination-prev").getAttribute("title");

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    req.resume();
    res.writeHead(401, { "content-type": "application/json" });
    res.end(JSON.stringify({ error: { message: "denied" } }));
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  baseURL = `http://127.0.0.1:${(server.address() as { port: number }).port}/anthropic`;
  await attach();
});
test.afterAll(async () => {
  // Later specs expect a Chinese instance.
  if (page) {
    await go("ResetDesktopQA").catch(() => {});
    await page.goto("http://wails.localhost/#/").catch(() => {});
    await page.reload().catch(() => {});
  }
  await browser?.close();
  await new Promise<void>((r) => server?.close(() => r()));
});

test("first start follows Windows: Japanese shows Japanese, German shows Chinese", async () => {
  await firstStart("ja-JP");
  await expect(
    page.getByRole("link", { name: "創作", exact: true }),
  ).toBeVisible();
  expect(await lang()).toBe("ja");
  await expect(page.locator(".rail-wordmark")).toHaveText("Image Workbench");
  await expect(page).toHaveTitle("創作 · Image Workbench");
  await expect(
    page.getByRole("button", { name: "表示言語：日本語", exact: true }),
  ).toBeVisible();
  expect(await go("InterfaceLanguage")).toBe("ja");

  await firstStart("de-DE");
  await expect(
    page.getByRole("link", { name: "创作", exact: true }),
  ).toBeVisible();
  expect(await lang()).toBe("zh-CN");
  await expect(page.locator(".rail-wordmark")).toHaveText("生图工作台");
  await expect(page).toHaveTitle("创作 · 生图工作台");
});

test("rail and settings switch the language at once, each named in its own script", async () => {
  await firstStart("zh-CN");
  await page
    .getByRole("button", { name: "界面语言：中文", exact: true })
    .click();
  const items = page.getByRole("menuitem");
  await expect(items).toHaveText([
    "中文",
    "English",
    "Tiếng Việt",
    "日本語",
    "한국어",
  ]);
  for (const [name, code] of [
    ["中文", "zh-CN"],
    ["English", "en"],
    ["Tiếng Việt", "vi"],
    ["日本語", "ja"],
    ["한국어", "ko"],
  ])
    await expect(
      page.getByRole("menuitem", { name }).locator(`[lang="${code}"]`),
    ).toHaveCount(1);
  await page.getByRole("menuitem", { name: "English" }).click();
  // The sidebar and navigation change without a restart.
  await expect(
    page.getByRole("link", { name: "Create", exact: true }),
  ).toBeVisible();
  for (const name of ["Pro mode", "Templates", "History", "Works & assets"])
    await expect(page.getByRole("link", { name, exact: true })).toBeVisible();
  expect(await lang()).toBe("en");
  await expect(page.locator(".rail-wordmark")).toHaveText("Image Workbench");
  await expect(
    page.getByRole("navigation", { name: "Main navigation" }),
  ).toBeVisible();
  await expect(page.locator(".rail-version")).toContainText("Version ");
  await page.getByRole("link", { name: "System", exact: true }).click();
  await expect(page).toHaveTitle("Connections · Image Workbench");
  for (const name of ["Connections", "Service status", "Model status", "Logs"])
    await expect(page.getByRole("link", { name, exact: true })).toBeVisible();

  // The settings page offers the same five, in their own scripts.
  const group = page.getByRole("radiogroup", { name: "Display language" });
  await expect(group.getByRole("radio")).toHaveCount(5);
  await group.getByText("Tiếng Việt", { exact: true }).click();
  await expect(
    page.getByRole("link", { name: "Sáng tác", exact: true }),
  ).toBeVisible();
  expect(await lang()).toBe("vi");
  await expect(
    page
      .getByRole("radiogroup", { name: "Ngôn ngữ giao diện" })
      .getByRole("radio", { name: "Tiếng Việt" }),
  ).toBeChecked();
  expect(await go("InterfaceLanguage")).toBe("vi");
  // Vietnamese is the longest: the rail and settings fit a narrow window.
  await page.setViewportSize({ width: 800, height: 900 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  const a11y = await new AxeBuilder({ page })
    .include(".rail")
    .include(".language-panel")
    .include(".system-tabs")
    .analyze();
  expect(a11y.violations).toEqual([]);
  await page.setViewportSize({ width: 1414, height: 1000 });
});

test("Ant Design text, dates and fonts follow the language; no missing glyphs", async () => {
  // Enough log entries for the log table to page.
  for (let i = 0; i < 52; i++)
    await go("TestConnection", {
      id: "lang-log-" + i,
      kind: "llm",
      baseURL,
      key: "lang-key",
      authMode: "api-key",
    });
  await page.getByRole("link", { name: "Nhật ký", exact: true }).click();
  await expect(page.locator(".ant-pagination-prev")).toBeVisible();
  expect(await prevPage()).toBe("Trang Trước");
  const firstTime = () =>
    page.locator("tbody .ant-table-row").first().locator("td").first();

  const expected = {
    ko: {
      prev: "이전 페이지",
      time: /오전|오후/,
      font: "Malgun Gothic",
      sample: "한국어 표시 언어",
    },
    en: {
      prev: "Previous Page",
      time: /AM|PM/,
      font: "Segoe UI",
      sample: "Display language",
    },
    ja: {
      prev: "前のページ",
      time: /\d{2}\/\d{2} \d{2}:\d{2}/,
      font: "Yu Gothic UI",
      sample: "日本語 ひらがな カタカナ",
    },
    vi: {
      prev: "Trang Trước",
      // Vietnamese puts the time first: 07:41 19-11.
      time: /^\d{2}:\d{2} \d{2}-\d{2}$/,
      font: "Segoe UI",
      sample: "Tiếng Việt Ở ữ ặ ệ",
    },
    "zh-CN": {
      prev: "上一页",
      time: /\d{2}\/\d{2} \d{2}:\d{2}/,
      font: "Microsoft YaHei UI",
      sample: "中文 界面语言",
    },
  } as const;
  const native = {
    ko: "한국어",
    en: "English",
    ja: "日本語",
    vi: "Tiếng Việt",
    "zh-CN": "中文",
  } as const;
  for (const code of ["ko", "en", "ja", "vi", "zh-CN"] as const) {
    await page.locator(".rail-language").click();
    await page.getByRole("menuitem", { name: native[code] }).click();
    await expect.poll(lang).toBe(code);
    await expect(page.locator(".ant-pagination-prev")).toBeVisible();
    expect(await prevPage()).toBe(expected[code].prev);
    // Nothing in the rail or the system tabs is cut off.
    expect(
      await page.evaluate(() =>
        [
          ...document.querySelectorAll(
            ".rail-label, .rail-wordmark, .rail-version, .system-tabs a",
          ),
        ]
          .filter((e) => e.scrollWidth > e.clientWidth + 0.5)
          .map((e) => e.textContent),
      ),
    ).toEqual([]);
    await expect(firstTime()).toHaveText(expected[code].time);
    const family = await page
      .locator(".rail-language .rail-label")
      .evaluate((e) => getComputedStyle(e).fontFamily);
    expect(family.replaceAll('"', "")).toMatch(
      new RegExp("^" + expected[code].font),
    );
    // The first font of the stack is installed and draws the text itself
    // (Vietnamese tone marks, kana, hangul), not a fallback.
    const own = await page.evaluate(
      ([font, sample]) => {
        const c = document.createElement("canvas").getContext("2d")!;
        c.font = `16px monospace`;
        const fallback = c.measureText(sample).width;
        c.font = `16px "${font}", monospace`;
        return c.measureText(sample).width !== fallback;
      },
      [expected[code].font, expected[code].sample],
    );
    expect(own, expected[code].font).toBe(true);
    await page.screenshot({
      path: path.join(root, "docs", `desktop-language-${code}.png`),
    });
  }
});

test("the chosen language is kept after the program restarts", async () => {
  await page.locator(".rail-language").click();
  await page.getByRole("menuitem", { name: "한국어" }).click();
  await expect(
    page.getByRole("link", { name: "창작", exact: true }),
  ).toBeVisible();
  const { dataDir } = await go<{ dataDir: string }>("Dashboard");
  const old = Number(fs.readFileSync(pidFile, "utf8").replace(/^﻿/, "").trim());
  // Quit for real, then start the same program on the same data, now on a
  // German Windows: the saved choice must win over the system language.
  void page.evaluate(() => window.go!.main!.App!.ForceQuit()).catch(() => {});
  await expect
    .poll(
      () => {
        try {
          process.kill(old, 0);
          return "running";
        } catch {
          return "exited";
        }
      },
      { timeout: 20000 },
    )
    .toBe("exited");
  await browser.close().catch(() => {});
  const next = spawn(exe, [], {
    cwd: path.dirname(exe),
    env: {
      ...process.env,
      IMAGEWORKBENCH_DATA_DIR: dataDir,
      IMAGEWORKBENCH_QA_SYSTEM_LANGUAGE: "de-DE",
    },
    detached: true,
    stdio: "ignore",
    windowsHide: true,
  });
  next.unref();
  fs.writeFileSync(pidFile, String(next.pid));
  await expect
    .poll(
      async () => {
        try {
          await attach();
          return true;
        } catch {
          return false;
        }
      },
      { timeout: 30000, intervals: [500] },
    )
    .toBe(true);
  await expect(
    page.getByRole("link", { name: "창작", exact: true }),
  ).toBeVisible();
  expect(await lang()).toBe("ko");
  expect(await go("InterfaceLanguage")).toBe("ko");
  await expect(page.locator(".rail-wordmark")).toHaveText("Image Workbench");
});

test("switching keeps what is on the page: typed requirement stays, page in the new language", async () => {
  await firstStart("zh-CN");
  await page.goto("http://wails.localhost/#/");
  const field = page.getByLabel("完整制作指令", { exact: true });
  await expect(field).toBeEditable();
  const typed = "保温杯，白底主图 + 场景图 4 张";
  await field.fill(typed);
  // Mark the element: a re-mounted page would have a new one.
  await field.evaluate(
    (el) => ((el as unknown as { qaMark: number }).qaMark = 1),
  );
  await page
    .getByRole("button", { name: "界面语言：中文", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "English" }).click();
  await expect(
    page.getByRole("link", { name: "Create", exact: true }),
  ).toBeVisible();
  const after = page.getByLabel("Full instructions", { exact: true });
  await expect(after).toHaveValue(typed);
  expect(
    await after.evaluate((el) => (el as unknown as { qaMark?: number }).qaMark),
  ).toBe(1);
  await expect(page).toHaveTitle("Create · Image Workbench");
  await expect(
    page.getByRole("heading", {
      name: "What would you like to make?",
      level: 1,
    }),
  ).toBeVisible();
  // And back: still the same text, now in Chinese.
  await page
    .getByRole("button", { name: "Display language: English", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "中文" }).click();
  await expect(page.getByLabel("完整制作指令", { exact: true })).toHaveValue(
    typed,
  );
});
