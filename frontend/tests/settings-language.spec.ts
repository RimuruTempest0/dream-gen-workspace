// 界面语言 (0.8.0) on the system pages: connections and settings, the hosted
// licence (trial, expiry, renewal), updates, both monitors, logs, templates and
// shop rules show no Chinese interface text in English, Vietnamese, Japanese
// and Korean. Text the backend writes (#7/#8) and stored text (ADR 0004) are
// not checked here. All upstreams are local mocks.
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
const areas = [
  "settings",
  "gateways",
  "agentSettings",
  "license",
  "updates",
  "monitor",
  "logs",
  "templates",
  "shopRules",
];
const table = {} as Record<Lang, Record<string, string>>;
for (const a of areas) {
  const t = JSON.parse(
    fs.readFileSync(path.join(messages, a + ".json"), "utf8"),
  ) as Tables;
  for (const l of Object.keys(t) as Lang[]) table[l] = { ...table[l], ...t[l] };
}
// The text for key in lang with {name} placeholders filled.
function tr(lang: Lang, key: string, params: Record<string, string> = {}) {
  return table[lang][key].replace(/\{(\w+)\}/g, (m, n) => params[n] ?? m);
}
const han = /[一-鿿]/;
// Chinese pieces of this ticket's texts; any of them on screen is a leftover.
// Pieces the target language writes the same way (Japanese 正常) are allowed.
function leftovers(lang: Lang) {
  const own = Object.values(table[lang]).join("\n");
  const pieces = new Set<string>();
  for (const v of Object.values(table["zh-CN"]))
    for (const p of v.split(/\{\w+\}/)) {
      const s = p.trim();
      if (s.length >= 2 && han.test(s) && !own.includes(s)) pieces.add(s);
    }
  return [...pieces];
}

let browser: Browser, page: Page, server: http.Server, base: string;
let updates: { manifest: Buffer; pkg: Buffer } = {
  manifest: Buffer.alloc(0),
  pkg: Buffer.alloc(0),
};
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
}
// Interface text on screen: visible text plus the names screen readers and
// tooltips read. Text the backend wrote is skipped: log entries, upstream and
// licence messages in alerts, connection test results.
async function screenText(scope = "body") {
  return page.evaluate((scope) => {
    const skip =
      ".log-message, .ant-alert-description, .license-panel .ant-alert-message, .ant-notification";
    const root = document.querySelector(scope)!;
    const parts: string[] = [];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const el = n.parentElement!;
      if (el.closest(skip) || !el.checkVisibility()) continue;
      parts.push(n.textContent || "");
    }
    for (const el of root.querySelectorAll(
      "[aria-label], [placeholder], [title]",
    )) {
      if (el.closest(skip)) continue;
      for (const a of ["aria-label", "placeholder", "title"])
        if (el.getAttribute(a)) parts.push(el.getAttribute(a)!);
    }
    return parts.join("\n");
  }, scope);
}
async function expectNoChinese(lang: Lang, where: string, scope = "main") {
  const text = await screenText(scope);
  const found = leftovers(lang).filter((p) => text.includes(p));
  expect(found, `${lang} ${where}: Chinese interface text`).toEqual([]);
  // English, Vietnamese and Korean have no Han characters at all.
  if (lang !== "ja") {
    const lines = text.split("\n").filter((l) => han.test(l));
    expect(lines, `${lang} ${where}: Han characters`).toEqual([]);
  }
}
async function a11y(include = "main") {
  await page.waitForFunction(() => document.getAnimations().length === 0);
  const scan = await new AxeBuilder({ page })
    .include(include)
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(scan.violations).toEqual([]);
}
async function noSideScroll() {
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth,
    ),
  ).toBe(true);
}

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    req.resume();
    if (req.url?.endsWith("/update.json")) {
      res.setHeader("content-type", "application/json");
      res.end(updates.manifest);
      return;
    }
    if (req.url?.endsWith(".zip")) {
      res.setHeader("content-type", "application/zip");
      res.end(updates.pkg);
      return;
    }
    res.setHeader("content-type", "application/json");
    if (req.url?.endsWith("/models")) {
      res.end(JSON.stringify({ data: [{ id: "mock-image" }] }));
      return;
    }
    res.end(JSON.stringify({ data: [{ b64_json: png }] }));
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  base = `http://127.0.0.1:${(server.address() as { port: number }).port}`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
});
test.afterAll(async () => {
  // Later specs expect a Chinese instance in developer mode.
  if (page) {
    await go("StartTestUpdate", "").catch(() => {});
    await go("ResetDesktopQA").catch(() => {});
    await page.goto("http://wails.localhost/#/").catch(() => {});
    await page.reload().catch(() => {});
  }
  await browser?.close();
  await new Promise<void>((r) => server?.close(() => r()));
});

for (const lang of ["en", "vi", "ja", "ko"] as const) {
  test(`${lang}: connections, templates, monitors and logs`, async () => {
    await go("ResetDesktopQA");
    await go("SetInterfaceLanguage", lang);
    const image = await go<{ id: string }>("SaveConnection", {
      name: "Image A",
      baseURL: `${base}/v1`,
      model: "mock-image",
      key: "test-key",
    });
    await go("SetGatewaySettings", {
      primaryID: image.id,
      backupIDs: [],
      autoReroute: true,
    });
    await go("SaveTemplate", { name: "Tpl", prompt: "a red apple" });
    await go("SaveShopRule", {
      id: "",
      name: "Rule",
      text: "white background",
      enabled: true,
    });
    // One real call so the monitor has a card, a bar and a table row.
    await go("Submit", {
      requestID: crypto.randomUUID(),
      connectionID: image.id,
      model: "mock-image",
      prompts: ["a red apple"],
      count: 1,
      references: [],
      size: "auto",
      quality: "auto",
      projectID: "",
    });

    await open("/settings");
    await expect(
      page.getByRole("heading", { name: tr(lang, "settings.title"), level: 1 }),
    ).toBeVisible();
    await expect(
      page.locator(".connection-row").filter({ hasText: "Image A" }),
    ).toBeVisible();
    await expect(
      page.locator(".connection-row").filter({ hasText: "Rule" }),
    ).toBeVisible();
    await expect(page.locator("#update")).toContainText(
      tr(lang, "updates.title"),
    );
    await expectNoChinese(lang, "settings");
    await noSideScroll();
    await a11y();

    // The image service dialog, with its form labels and hints.
    await page
      .getByRole("button", { name: tr(lang, "settings.add"), exact: true })
      .click();
    const dialog = page.getByRole("dialog", {
      name: tr(lang, "settings.add.title"),
    });
    await expect(dialog).toBeVisible();
    await expectNoChinese(lang, "connection dialog", ".ant-modal");
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();

    await open("/templates");
    await expect(
      page.getByRole("heading", {
        name: tr(lang, "templates.title"),
        level: 1,
      }),
    ).toBeVisible();
    await expect(page.getByText("Tpl", { exact: true })).toBeVisible();
    await expectNoChinese(lang, "templates");
    await a11y();

    await open("/monitor/gateways");
    await expect(
      page.getByRole("heading", { name: tr(lang, "monitor.gw.title") }),
    ).toBeVisible();
    // Wait for the call to be counted: one row in the call records.
    await expect(page.locator(".monitor-card")).toContainText("Image A");
    await expect(page.locator(".ant-table-row").first()).toBeVisible({
      timeout: 20000,
    });
    await expect(
      page.getByRole("button", { name: tr(lang, "monitor.export") }),
    ).toBeVisible();
    await expect(page.locator(".day-bars")).toHaveAttribute(
      "aria-label",
      new RegExp("^" + tr(lang, "monitor.gw.daily.aria", { days: "" }).trim()),
    );
    await expect(page.locator(".ant-table-thead")).toContainText(
      tr(lang, "monitor.col.result"),
    );
    await expectNoChinese(lang, "service status");
    await noSideScroll();
    await a11y();

    await open("/monitor/llm");
    await expect(
      page.getByRole("heading", { name: tr(lang, "monitor.llm.title") }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: tr(lang, "monitor.export") }),
    ).toBeVisible();
    await expectNoChinese(lang, "model status");
    await a11y();

    await open("/logs");
    await expect(
      page.getByRole("heading", { name: tr(lang, "logs.title"), level: 1 }),
    ).toBeVisible();
    await expect(page.locator(".ant-table-row").first()).toBeVisible();
    await expectNoChinese(lang, "logs");
    await a11y();
  });

  test(`${lang}: hosted licence trial, expiry, renewal and the update offer`, async () => {
    // Trial: badge and panel in the chosen language.
    const customer = await go<{ softwareID: string }>(
      "StartTestCustomer",
      `${base}/v1`,
      `${base}/v1`,
      2,
    );
    await go("SetInterfaceLanguage", lang);
    await open("/settings");
    const badge = page.locator(".license-badge");
    await expect(badge).toHaveText(
      tr(lang, "license.badge.trial", { left: "2", limit: "2" }),
    );
    const panel = page.locator("#license");
    await expect(panel).toContainText(customer.softwareID);
    await expect(
      panel.getByText(tr(lang, "license.state.trial"), { exact: true }),
    ).toBeVisible();
    await expect(
      panel.getByRole("button", { name: tr(lang, "license.activate") }),
    ).toBeVisible();
    await expectNoChinese(lang, "hosted trial");
    await a11y();

    // Expired: the badge says so.
    const past = new Date(Date.now() - 3600_000).toISOString();
    await go("StartTestCustomerExpiring", `${base}/v1`, `${base}/v1`, 5, past);
    await go("SetInterfaceLanguage", lang);
    await open("/settings");
    await expect(badge).toHaveText(tr(lang, "license.badge.expired"));
    await expect(
      panel.getByText(tr(lang, "license.state.expired"), { exact: true }),
    ).toBeVisible();
    await expectNoChinese(lang, "licence expired");

    // Renewed for three days: the warning asks to renew, in this language.
    const soon = new Date(Date.now() + 3 * 86400_000 - 3600_000).toISOString();
    const renewal = await go<string>("WriteTestRenewal", soon);
    await go("ImportCustomerFile", renewal);
    await open("/settings");
    await expect(
      panel.getByText(tr(lang, "license.expiring", { days: "3" })),
    ).toBeVisible();
    await expectNoChinese(lang, "licence expiring");

    // The update offer: rail line and dialog chrome (the notes are #10).
    const files = await go<{ manifest: string; package: string }>(
      "StartTestUpdate",
      base,
    );
    updates = {
      manifest: Buffer.from(files.manifest, "base64"),
      pkg: Buffer.from(files.package, "base64"),
    };
    await open("/settings");
    await page
      .locator("#update")
      .getByRole("button", { name: tr(lang, "updates.check") })
      .click();
    const offer = page.locator(".update-badge");
    await expect(offer).toHaveText(
      tr(lang, "updates.badge", { version: "9.9.9" }),
    );
    await offer.click();
    const dialog = page.getByRole("dialog", {
      name: tr(lang, "updates.title"),
    });
    await expect(
      dialog.getByRole("button", { name: tr(lang, "updates.offer.install") }),
    ).toBeVisible();
    await expectNoChinese(
      lang,
      "update dialog",
      ".ant-modal .ant-modal-header",
    );
    await expect(dialog.locator(".hint").last()).toHaveText(
      tr(lang, "updates.offer.hint"),
    );
    await page.keyboard.press("Escape");
    await go("StartTestUpdate", "");
  });
}
