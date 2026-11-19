// 界面语言 (0.8.0) acceptance sweep: in each of the five languages every page
// of the program (create, pro mode, templates, history, works, and the
// system tabs) shows no leftover Chinese outside data, has no horizontal
// scroll in a narrow window, and passes the axe check. Data here is ASCII;
// the only Chinese data is stored log text (ADR 0004: written in the language
// of its moment) and each language's own name in the language switch. Checks
// are soft so one run lists every page's problems.
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
test.describe.configure({ mode: "serial" });

type Lang = "zh-CN" | "en" | "vi" | "ja" | "ko";
type Tables = Record<Lang, Record<string, string>>;
const messages = path.resolve(import.meta.dirname, "../src/i18n/messages");
const table = {} as Tables;
for (const f of fs.readdirSync(messages).filter((f) => f.endsWith(".json"))) {
  const t = JSON.parse(
    fs.readFileSync(path.join(messages, f), "utf8"),
  ) as Tables;
  for (const l of Object.keys(t) as Lang[]) table[l] = { ...table[l], ...t[l] };
}
const han = /\p{Script=Han}/u;
// Chinese pieces of the interface text (all tables) that a language does not
// write the same way; any of them on screen is a leftover. Japanese shares
// Han characters, so for it only these whole pieces count.
function leftovers(lang: Lang) {
  const own = Object.values(table[lang]).join("\n");
  const pieces = new Set<string>();
  for (const v of Object.values(table["zh-CN"]))
    for (const p of v.split(/\{\w+\}/)) {
      const s = p.trim();
      if (s.length >= 3 && han.test(s) && !own.includes(s)) pieces.add(s);
    }
  return [...pieces];
}
// Each language's own name, shown in its own script in the language switch.
const languageNames = ["中文", "日本語"];

const pages = [
  { route: "/", name: "create" },
  { route: "/pro", name: "pro mode" },
  { route: "/templates", name: "templates" },
  { route: "/tasks", name: "history" },
  { route: "/assets", name: "works" },
  { route: "/settings", name: "connections" },
  { route: "/monitor/gateways", name: "service status" },
  { route: "/monitor/llm", name: "model status" },
  { route: "/logs", name: "logs" },
];

let browser: Browser, page: Page, server: http.Server, base: string;
const png = fs
  .readFileSync(path.resolve(import.meta.dirname, "fixture.png"))
  .toString("base64");
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
    [name, args] as const,
  ) as Promise<T>;
}
async function open(route: string) {
  await page.goto(`http://wails.localhost/#${route}`);
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect(page.locator("main h1").first()).toBeVisible();
  // Let the page's data arrive and the loading spinners go.
  await expect(page.locator("main .ant-spin-spinning")).toHaveCount(0, {
    timeout: 10000,
  });
}
// Visible text of the whole window (rail and page) plus the names screen
// readers and tooltips read; stored log text is data.
async function screenText() {
  return page.evaluate(() => {
    const skip = ".log-message, .log-detail";
    const parts: string[] = [];
    const walker = document.createTreeWalker(
      document.body,
      NodeFilter.SHOW_TEXT,
    );
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const el = n.parentElement!;
      if (el.closest(skip) || !el.checkVisibility()) continue;
      parts.push(n.textContent || "");
    }
    for (const el of document.body.querySelectorAll(
      "[aria-label], [placeholder], [title], [alt]",
    )) {
      if (el.closest(skip)) continue;
      for (const a of ["aria-label", "placeholder", "title", "alt"])
        if (el.getAttribute(a)) parts.push(el.getAttribute(a)!);
    }
    return parts.join("\n");
  });
}
async function expectNoChinese(lang: Lang, where: string) {
  const text = languageNames.reduce(
    (v, w) => v.split(w).join(""),
    await screenText(),
  );
  expect
    .soft(
      leftovers(lang).filter((p) => text.includes(p)),
      `${lang} ${where}: Chinese interface text`,
    )
    .toEqual([]);
  if (lang !== "ja")
    expect
      .soft(
        text.split("\n").filter((l) => han.test(l)),
        `${lang} ${where}: Han characters`,
      )
      .toEqual([]);
}
async function a11y(where: string) {
  await page.mouse.move(0, 0);
  await page.waitForFunction(() =>
    document
      .getAnimations()
      .every((a) => a.effect?.getComputedTiming().endTime === Infinity),
  );
  const scan = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect
    .soft(
      scan.violations.map((v) => ({
        id: v.id,
        nodes: v.nodes.map((n) => `${n.target.join(" ")}: ${n.failureSummary}`),
      })),
      where,
    )
    .toEqual([]);
}
async function noSideScroll(where: string) {
  await page.setViewportSize({ width: 800, height: 900 });
  await page.waitForTimeout(200);
  expect
    .soft(
      await page.evaluate(
        () =>
          document.documentElement.scrollWidth <=
          document.documentElement.clientWidth,
      ),
      `${where} at 800px`,
    )
    .toBe(true);
  await page.setViewportSize({ width: 1414, height: 1000 });
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
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  base = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  await go("ResetDesktopQA");
  await page.setViewportSize({ width: 1414, height: 1000 });
  // A connection, a template and a few images so no page is empty.
  const c = await go<{ id: string }>("SaveConnection", {
    name: "QA images",
    baseURL: `${base}/v1`,
    model: "qa-image",
    key: "qa-key-123",
  });
  await go("SaveTemplate", { name: "QA template", prompt: "a red apple" });
  await go("Submit", {
    requestID: crypto.randomUUID(),
    connectionID: c.id,
    model: "qa-image",
    prompts: ["every page qa"],
    count: 2,
    references: [],
    size: "1024x1024",
    quality: "high",
    projectID: "",
  });
  await expect
    .poll(
      async () =>
        (await go<{ total: number }>("ListTasks", "succeeded", 0, 20)).total,
      { timeout: 30000 },
    )
    .toBe(2);
});
test.afterAll(async () => {
  // Later specs expect a Chinese instance.
  if (page) {
    await page.setViewportSize({ width: 1414, height: 1000 }).catch(() => {});
    await go("SetInterfaceLanguage", "zh-CN").catch(() => {});
    await go("ResetDesktopQA").catch(() => {});
    await page.goto("http://wails.localhost/#/").catch(() => {});
    await page.reload().catch(() => {});
  }
  await browser?.close();
  await new Promise<void>((r) => server?.close(() => r()));
});

for (const lang of ["zh-CN", "en", "vi", "ja", "ko"] as const) {
  test(`${lang}: every page is in the language, fits 800px and passes axe`, async () => {
    test.setTimeout(120000);
    await go("SetInterfaceLanguage", lang);
    for (const p of pages) {
      await open(p.route);
      await expect
        .poll(() => page.evaluate(() => document.documentElement.lang))
        .toBe(lang);
      if (lang !== "zh-CN") await expectNoChinese(lang, p.name);
      await a11y(`${lang} ${p.name}`);
      await noSideScroll(`${lang} ${p.name}`);
    }
  });
}
