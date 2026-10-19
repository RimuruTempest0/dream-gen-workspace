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
import { execFileSync } from "node:child_process";
const docs = path.resolve(import.meta.dirname, "../../docs");
test.describe.configure({ mode: "serial" });

let browser: Browser,
  page: Page,
  server: http.Server,
  baseURL: string,
  requests = 0;
let modelMode = "normal",
  modelRequests = 0;
const png = fs
  .readFileSync(path.resolve(import.meta.dirname, "fixture.png"))
  .toString("base64");
test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    if (req.url === "/custom/v1/models") {
      modelRequests++;
      if (modelMode === "denied") {
        res.writeHead(401);
        res.end(JSON.stringify({ error: { message: "qa-key-123 denied" } }));
        return;
      }
      if (modelMode === "slow") {
        const timer = setTimeout(
          () => res.end(JSON.stringify({ data: [{ id: "qa-image" }] })),
          3000,
        );
        res.on("close", () => clearTimeout(timer));
        return;
      }
      res.setHeader("content-type", "application/json");
      if (modelMode === "stream") {
        res.write(JSON.stringify({ data: [{ id: "qa-image" }] }));
        const timer = setTimeout(() => res.end(), 3000);
        res.on("close", () => clearTimeout(timer));
        return;
      }
      res.end(JSON.stringify({ data: [{ id: "qa-image" }] }));
      return;
    }
    if (req.url === "/custom/v1/images/generations") {
      requests++;
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        const v = JSON.parse(body);
        if (v.prompt.includes("timeout")) {
          res.writeHead(503);
          res.end(
            JSON.stringify({
              error: { message: "qa-key-123 upstream unavailable" },
            }),
          );
          return;
        }
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ data: [{ b64_json: png }] }));
      });
      return;
    }
    res.writeHead(404);
    res.end();
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  const addr = server.address() as { port: number };
  baseURL = `http://127.0.0.1:${addr.port}/custom/v1`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  await page.evaluate(async()=>window.go!.main!.App!.ResetDesktopQA());
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await page.waitForLoadState("domcontentloaded");
});
test.afterAll(async () => {
  await browser?.close();
  await new Promise<void>((r) => server?.close(() => r()));
});
test("desktop connection CRUD, masked key, non-billable model test and prompt draft", async () => {
  await expect(
    page.getByRole("heading", { name: "把想法变成图片" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "连接与设置" }).click();
  await page.getByRole("button", { name: "添加连接", exact: true }).click();
  const modal = page.getByRole("dialog");
  await modal.getByRole("button", { name: "测试连接 / 获取模型" }).click();
  await expect(
    modal.getByText("请填写 Base URL", { exact: true }),
  ).toBeVisible();
  await modal.getByLabel("连接名称").fill("QA 中转站");
  await modal.getByLabel("Base URL", { exact: true }).fill(baseURL);
  const key = modal.getByLabel("API Key", { exact: true });
  await modal.getByRole("button", { name: "测试连接 / 获取模型" }).click();
  await expect(
    modal.getByText("请填写 API Key", { exact: true }),
  ).toBeVisible();
  await key.fill("qa-key-123");
  await expect(key).toHaveAttribute("type", "password");
  await modal.getByRole("button", { name: "显示 API Key" }).click();
  await expect(key).toHaveAttribute("type", "text");
  await modal.getByRole("button", { name: "隐藏 API Key" }).click();
  await modal.getByRole("button", { name: "测试连接 / 获取模型" }).click();
  await expect(modal.getByText("模型查询认证通过")).toBeVisible();
  expect(requests).toBe(0);
  await modal.getByLabel("默认模型 ID（可选）").fill("qa-image");
  await modal.getByRole("button", { name: "保存连接" }).click();
  await expect(modal).not.toBeVisible();
  await expect(page.getByText(baseURL, { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "生图工作台" }).click();
  await page.getByRole("combobox", { name: "中转站连接" }).click();
  await page.getByText("QA 中转站", { exact: true }).last().click();
  await page.getByLabel("提示词", { exact: true }).fill("桌面测试白底产品图");
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const a = window.go!.main!.App!;
        return ((await a.GetDraft()) as { prompts: string[] }).prompts[0];
      }),
    )
    .toBe("桌面测试白底产品图");
  await page.screenshot({
    path: path.join(docs, "desktop-workbench.png"),
    fullPage: true,
  });
});
test("real desktop generates locally and unknown completion never retries", async () => {
  await page.getByRole("button", { name: "生成图片", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "继续", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const r = (await window.go!.main!.App!.ListTasks("", 0, 10)) as {
          items: { status: string }[];
        };
        return r.items[0]?.status;
      }),
    )
    .toBe("succeeded");
  expect(requests).toBe(1);
  await page.getByLabel("提示词", { exact: true }).fill("timeout");
  await page.getByRole("button", { name: "生成图片", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "继续", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const r = (await window.go!.main!.App!.ListTasks("", 0, 10)) as {
          items: { status: string }[];
        };
        return r.items[0]?.status;
      }),
    )
    .toBe("unknown");
  await page.getByRole("link", { name: /任务中心/ }).click();
  await expect(page.getByText("结果未知", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "详情", exact: true }).first().click();
  await expect(
    page.getByRole("dialog").getByText("HTTP 503", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("dialog")).not.toContainText("qa-key-123");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  expect(requests).toBe(2);
});
test("project/template CRUD,素材归档 and keyboard select", async () => {
  await page.getByRole("link", { name: "项目与模板" }).click();
  await page.getByRole("button", { name: "新建项目" }).click();
  await page.getByLabel("名称", { exact: true }).fill("测试商品");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "保存", exact: true })
    .click();
  await expect(page.getByText("测试商品", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "新建模板" }).click();
  await page.getByLabel("名称", { exact: true }).fill("产品摄影");
  await page
    .getByLabel("提示词", { exact: true })
    .fill("white background product");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "保存", exact: true })
    .click();
  await expect(page.getByText("产品摄影", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "素材库" }).click();
  await expect(page.getByRole("button", { name: /预览/ })).toHaveCount(1);
  await page.getByRole("combobox", { name: "项目筛选" }).focus();
  await page.keyboard.press("ArrowDown");
  await expect(page.locator(".ant-select-dropdown:visible")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.screenshot({
    path: path.join(docs, "desktop-assets.png"),
    fullPage: true,
  });
});
test("accessibility and small viewport layout", async () => {
  await page.getByRole("link", { name: "连接与设置" }).click();
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  fs.writeFileSync(
    path.join(docs, "accessibility-results.json"),
    JSON.stringify(results.violations, null, 2),
  );
  expect(results.violations).toEqual([]);
  await page.getByRole("link", { name: "生图工作台" }).click();
  const session = await browser.contexts()[0].newCDPSession(page);
  await session.send("Emulation.setDeviceMetricsOverride", {
    width: 900,
    height: 740,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await expect(
    page.getByRole("button", { name: "生成图片", exact: true }),
  ).toBeVisible();
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth,
  );
  expect(overflow).toBe(false);
  await page.screenshot({
    path: path.join(docs, "desktop-narrow.png"),
    fullPage: true,
  });
  await session.send("Emulation.clearDeviceMetricsOverride");
  await session.detach();
});
test("保存位置：取消、拒绝非空目录、迁移数据与设置持久化", async () => {
  await page.getByRole("link", { name: "连接与设置" }).click();
  const open = page.getByRole("button", { name: "打开数据位置", exact: true });
  await expect(open).toBeEnabled();
  const change = page.getByRole("button", {
    name: "更改保存位置",
    exact: true,
  });
  await expect(change).toBeEnabled();
  const original = await page.evaluate(
    async () =>
      (await window.go!.main!.App!.Dashboard()) as { dataDir: string },
  );
  await page.evaluate(() => {
    const api = window.go!.main!.App!;
    (window as any).originalPicker = api.ChooseDataDirectory;
    api.ChooseDataDirectory = async () => "";
  });
  await change.click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  expect(
    (
      await page.evaluate(
        async () =>
          (await window.go!.main!.App!.Dashboard()) as { dataDir: string },
      )
    ).dataDir,
  ).toBe(original.dataDir);
  const tools = path.resolve(import.meta.dirname, "../../.tools");
  const occupied = fs.mkdtempSync(path.join(tools, "qa-occupied-"));
  fs.writeFileSync(path.join(occupied, "keep.txt"), "keep");
  await page.evaluate((folder) => {
    window.go!.main!.App!.ChooseDataDirectory = async () => folder;
  }, occupied);
  await change.click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "迁移并使用" })
    .click();
  await expect(page.getByText(/新保存目录必须为空/)).toBeVisible();
  expect(fs.readFileSync(path.join(occupied, "keep.txt"), "utf8")).toBe("keep");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  const target = fs.mkdtempSync(path.join(tools, "qa-selected-"));
  await page.evaluate((folder) => {
    window.go!.main!.App!.ChooseDataDirectory = async () => folder;
  }, target);
  await change.click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "迁移并使用" })
    .click();
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(page.locator(".storage-panel .data-path")).toHaveText(target);
  expect(fs.existsSync(path.join(original.dataDir, "app.db"))).toBe(true);
  expect(fs.existsSync(path.join(target, "app.db"))).toBe(true);
  const connections = await page.evaluate(
    async () =>
      (await window.go!.main!.App!.ListConnections()) as { hasKey: boolean }[],
  );
  expect(connections[0].hasKey).toBe(true);
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const r = (await window.go!.main!.App!.ListAssets("", 0, 24)) as {
          total: number;
        };
        return r.total;
      }),
    )
    .toBe(1);
  expect(
    fs.readFileSync(path.join(tools, "desktop-qa/data-location.txt"), "utf8"),
  ).toBe(target);
  await page.evaluate(() => {
    window.go!.main!.App!.ChooseDataDirectory = (window as any).originalPicker;
  });
  await page.screenshot({
    path: path.join(docs, "data-location-settings.png"),
    fullPage: true,
  });
});

test("connection test: saved key, complete JSON, auth failure and cancel", async () => {
  await page.getByRole("link", { name: "连接与设置" }).click();
  await page.getByRole("button", { name: "编辑 / 测试" }).click();
  const dialog = page.getByRole("dialog");
  const testButton = dialog.getByRole("button", {
    name: "测试连接 / 获取模型",
  });
  await expect(dialog.getByLabel("API Key", { exact: true })).toHaveValue("");
  modelMode = "stream";
  await testButton.click();
  await expect(dialog.getByText("模型查询认证通过")).toBeVisible({
    timeout: 2000,
  });
  await dialog.getByRole("combobox", { name: "查询到的模型" }).click();
  await expect(
    page
      .locator(".ant-select-dropdown:visible")
      .locator(".ant-select-item-option-content"),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  modelMode = "denied";
  await testButton.click();
  await expect(dialog.getByText(/认证被拒绝/)).toBeVisible();
  await expect(dialog).toContainText("HTTP 401");
  await expect(dialog).not.toContainText("qa-key-123");
  modelMode = "slow";
  const before = modelRequests;
  await testButton.click();
  await expect(dialog.getByRole("status")).toContainText("最多等待 30 秒");
  await expect.poll(() => modelRequests).toBe(before + 1);
  await dialog.getByRole("button", { name: "取消测试" }).click();
  await expect(testButton).toBeEnabled();
  await expect(
    dialog.getByRole("button", { name: "取消测试" }),
  ).not.toBeVisible();
  modelMode = "normal";
  await testButton.click();
  await expect(dialog.getByText("模型查询认证通过")).toBeVisible();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).not.toBeVisible();
});

test("open data location really shows Explorer folder", async () => {
  await page.getByRole("link", { name: "连接与设置" }).click();
  const summary = await page.evaluate(
    async () =>
      (await window.go!.main!.App!.Dashboard()) as { dataDir: string },
  );
  const script = path.resolve(
    import.meta.dirname,
    "../../scripts/qa-shell-folder.ps1",
  );
  const shellState = () =>
    execFileSync(
      "pwsh.exe",
      ["-NoProfile", "-File", script, "-Target", summary.dataDir],
      { encoding: "utf8", timeout: 8000, windowsHide: true },
    ).trim();
  expect(shellState()).toBe("absent");
  await page.getByRole("button", { name: "打开数据位置", exact: true }).click();
  await expect(page.getByText("已打开数据目录", { exact: true })).toBeVisible();
  await expect.poll(shellState, { timeout: 15000 }).toBe("visible");
  execFileSync(
    "pwsh.exe",
    ["-NoProfile", "-File", script, "-Target", summary.dataDir, "-Close"],
    { timeout: 8000, windowsHide: true },
  );
});

test("recent results: single download, whole ten-image set and cancelled export", async () => {
  await page.getByRole("link", { name: "生图工作台" }).click();
  await page.getByLabel("提示词", { exact: true }).fill("QA ten image set");
  await page.getByRole("spinbutton", { name: "每条生成张数" }).fill("10");
  const before = requests;
  await page
    .getByRole("button", { name: "生成 10 张图片", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "继续", exact: true })
    .click();
  await expect.poll(() => requests, { timeout: 25000 }).toBe(before + 10);
  const latest = page.locator(".recent-batch").first();
  await expect(
    latest.getByRole("button", { name: "下载套图（10 张）", exact: true }),
  ).toBeVisible({ timeout: 25000 });
  await expect(
    latest.getByRole("button", { name: "下载图片", exact: true }),
  ).toHaveCount(6);
  const output = fs.mkdtempSync(
    path.resolve(import.meta.dirname, "../../.tools/qa-download-"),
  );
  await page.evaluate(() => {
    (window as any).originalExportPicker =
      window.go!.main!.App!.ChooseExportPath;
    window.go!.main!.App!.ChooseExportPath = async () => "";
  });
  await latest
    .getByRole("button", { name: "下载套图（10 张）", exact: true })
    .click();
  await expect(
    latest.getByRole("button", { name: "下载套图（10 张）", exact: true }),
  ).toBeEnabled();
  expect(fs.readdirSync(output)).toEqual([]);
  const single = path.join(output, "single.png");
  await page.evaluate((p) => {
    window.go!.main!.App!.ChooseExportPath = async () => p;
  }, single);
  await latest
    .getByRole("button", { name: "下载图片", exact: true })
    .first()
    .click();
  await expect(page.getByText("图片已保存", { exact: true })).toBeVisible();
  expect(fs.readFileSync(single).toString("base64")).toBe(png);
  const set = path.join(output, "set.zip");
  await page.evaluate((p) => {
    window.go!.main!.App!.ChooseExportPath = async () => p;
  }, set);
  await latest
    .getByRole("button", { name: "下载套图（10 张）", exact: true })
    .click();
  await expect(
    page.getByText("已保存套图 ZIP（10 张）", { exact: true }),
  ).toBeVisible();
  const zip = fs.readFileSync(set);
  let entries = 0;
  for (let i = 0; i < zip.length - 4; i++) {
    if (zip.readUInt32LE(i) === 0x02014b50) entries++;
  }
  expect(entries).toBe(10);
  expect(requests).toBe(before + 10);
  const invalid = path.join(output, "missing-folder", "set.zip");
  await page.evaluate((p) => {
    window.go!.main!.App!.ChooseExportPath = async () => p;
  }, invalid);
  await latest
    .getByRole("button", { name: "下载套图（10 张）", exact: true })
    .click();
  await expect(page.getByText("操作未完成", { exact: true })).toBeVisible();
  expect(fs.existsSync(invalid)).toBe(false);
  await page.evaluate(() => {
    window.go!.main!.App!.ChooseExportPath = (
      window as any
    ).originalExportPicker;
  });
  await page.screenshot({
    path: path.join(docs, "recent-downloads.png"),
    fullPage: true,
  });
  const a11y = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);
  const session = await browser.contexts()[0].newCDPSession(page);
  await session.send("Emulation.setDeviceMetricsOverride", {
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
    path: path.join(docs, "recent-downloads-narrow.png"),
    fullPage: true,
  });
  await session.send("Emulation.clearDeviceMetricsOverride");
  await session.detach();
});
