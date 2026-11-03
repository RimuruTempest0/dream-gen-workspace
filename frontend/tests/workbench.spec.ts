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
const gatewayHits = { models: 0, gw524: 0, gwok: 0 };
const png = fs
  .readFileSync(path.resolve(import.meta.dirname, "fixture.png"))
  .toString("base64");
test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    // Two extra gateways for failover: one always cut by a 524, one healthy.
    const gateway = req.url?.match(
      /^\/(gw524|gwok)\/v1\/(models|images\/generations)$/,
    );
    if (gateway) {
      const [, name, path] = gateway;
      req.resume();
      if (path === "models") {
        gatewayHits.models++;
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ data: [{ id: "qa-image" }] }));
        return;
      }
      gatewayHits[name as "gw524" | "gwok"]++;
      setTimeout(() => {
        if (name === "gw524") {
          res.writeHead(524);
          res.end("error code: 524");
          return;
        }
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ data: [{ b64_json: png }] }));
      }, 50);
      return;
    }
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
      res.end(
        JSON.stringify({ data: [{ id: "qa-image" }, { id: "qa-image-2" }] }),
      );
      return;
    }
    if (req.url === "/custom/v1/images/generations") {
      requests++;
      let body = "";
      req.on("data", (c) => (body += c));
      req.on("end", () => {
        const v = JSON.parse(body);
        // Never answers, like a relay that keeps an image waiting.
        if (v.prompt.includes("hang")) return;
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
  await page.evaluate(async () => window.go!.main!.App!.ResetDesktopQA());
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
test("real desktop generates locally and an unknown result is re-generated once", async () => {
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
  // The unknown image is re-generated automatically once, never twice.
  await expect
    .poll(() =>
      page.evaluate(async () => {
        const r = (await window.go!.main!.App!.ListTasks("", 0, 10)) as {
          items: { status: string; autoRetryOf?: string }[];
        };
        return `${r.items[0]?.status}:${!!r.items[0]?.autoRetryOf}:${r.items[1]?.status}`;
      }),
    )
    .toBe("unknown:true:unknown");
  await page.waitForTimeout(1500);
  expect(requests).toBe(3);
  await page.getByRole("link", { name: /任务中心/ }).click();
  await expect(
    page.getByText("结果未知", { exact: true }).first(),
  ).toBeVisible();
  await page.getByRole("button", { name: "详情", exact: true }).first().click();
  await expect(
    page.getByRole("dialog").getByText("HTTP 503", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("dialog")).not.toContainText("qa-key-123");
  await expect(page.getByRole("dialog")).toContainText("不会再次自动重发");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  expect(requests).toBe(3);
});
test("an image waiting on the relay can be cancelled after confirming", async () => {
  await page.getByRole("link", { name: "生图工作台" }).click();
  await page.getByLabel("提示词", { exact: true }).fill("hang");
  await page.getByRole("button", { name: "生成图片", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "继续", exact: true })
    .click();
  await expect.poll(() => requests).toBe(4);
  await page.getByRole("link", { name: /任务中心/ }).click();
  const row = page.getByRole("row").filter({ hasText: "等待中转站" });
  await row.getByRole("button", { name: "取消生成" }).click();
  const dialog = page.getByRole("dialog", { name: "是否确认取消生成？" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("中转站可能仍会生成并计费");
  // Keeping it is the default choice.
  await dialog.getByRole("button", { name: "继续生成" }).click();
  await expect(dialog).toBeHidden();
  await expect(row.getByText("等待中转站")).toBeVisible();
  await row.getByRole("button", { name: "取消生成" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消生成", exact: true })
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
    .toBe("canceled");
  await expect(
    page.getByRole("row").first().getByRole("button", { name: "取消生成" }),
  ).toHaveCount(0);
  expect(requests).toBe(4);
});
test("settings no longer offer a detail level for the smart page", async () => {
  await page.getByRole("link", { name: "连接与设置" }).click();
  await expect(
    page.getByRole("heading", { name: "本地数据与备份" }),
  ).toBeVisible();
  await expect(page.getByText("制作详情显示")).toHaveCount(0);
});
test("workbench picks the model from the gateway's list and sizes from options", async () => {
  await page.getByRole("link", { name: "生图工作台" }).click();
  const field = (name: string) =>
    page
      .getByRole("combobox", { name, exact: true })
      .locator("..")
      .locator("..");
  await field("模型").click();
  const open = page.locator(
    ".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option",
  );
  await expect(open).toHaveText([
    "使用连接默认（qa-image）",
    "qa-image",
    "qa-image-2",
  ]);
  await open.filter({ hasText: "qa-image-2" }).click();
  await expect
    .poll(
      async () =>
        (
          (await page.evaluate(() => window.go!.main!.App!.GetDraft())) as {
            model: string;
          }
        ).model,
    )
    .toBe("qa-image-2");
  await field("模型").click();
  await open.first().click();
  await page
    .getByLabel("尺寸", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await expect(open.filter({ hasText: "1024×1024（1:1 方图）" })).toBeVisible();
  await page.keyboard.press("Escape");
  // Projects are gone from every page; existing images stay.
  await expect(page.getByLabel("归属项目")).toHaveCount(0);
  await expect(page.getByRole("link", { name: /项目/ })).toHaveCount(0);
});
test("prompt templates: saved once, used from both generation pages without losing text", async () => {
  await page.getByRole("link", { name: "提示词模板" }).click();
  await expect(
    page.getByRole("heading", { name: "提示词模板", level: 1 }),
  ).toBeVisible();
  await page.getByRole("button", { name: "新建模板" }).first().click();
  const editor = page.getByRole("dialog", { name: "保存为模板" });
  await editor.getByLabel("名称", { exact: true }).fill("产品摄影");
  await editor
    .getByLabel("提示词", { exact: true })
    .fill("white background product");
  await editor.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByText("产品摄影", { exact: true })).toBeVisible();
  const prompt = page.getByLabel(/^提示词/);
  // From the template page into the workbench: existing text is not overwritten silently.
  await page.getByRole("button", { name: "用于生图工作台" }).click();
  const choose = page.getByRole("dialog", { name: /套用模板/ });
  await expect(choose).toBeVisible();
  await choose.getByRole("button", { name: "取消" }).click();
  await expect(choose).toBeHidden();
  await expect(prompt).not.toHaveValue("white background product");
  await prompt.fill("我写的内容");
  const pick = async (name: string) => {
    await page.getByRole("combobox", { name: "套用模板" }).click();
    await page
      .locator(
        ".ant-select-dropdown:not(.ant-select-dropdown-hidden) .template-option",
      )
      .filter({ hasText: name })
      .click();
  };
  await pick("产品摄影");
  await choose.getByRole("button", { name: "追加到末尾" }).click();
  await expect(prompt).toHaveValue("我写的内容\n\nwhite background product");
  await pick("产品摄影");
  await choose.getByRole("button", { name: "替换当前内容" }).click();
  await expect(prompt).toHaveValue("white background product");
  // An empty box takes the template directly, with no question.
  await prompt.fill("");
  await pick("产品摄影");
  await expect(choose).toBeHidden();
  await expect(prompt).toHaveValue("white background product");
  // In batch mode an appended template stays one line, so it stays one image.
  await page.getByRole("switch", { name: "批量提示词" }).click();
  await prompt.fill("第一条");
  await pick("产品摄影");
  await choose.getByRole("button", { name: "追加到末尾" }).click();
  await expect(prompt).toHaveValue("第一条\nwhite background product");
  // Current text saves as a new template, including a link for the next page.
  await prompt.fill("https://shop.example/item/9 做一套主图");
  await page.getByRole("button", { name: "存为模板" }).first().click();
  const save = page.getByRole("dialog", { name: "保存为模板" });
  await expect(save.getByLabel("提示词", { exact: true })).toHaveValue(
    "https://shop.example/item/9 做一套主图",
  );
  await save.getByLabel("名称", { exact: true }).fill("链接模板");
  await save.getByRole("button", { name: "保存", exact: true }).click();
  await expect(save).toBeHidden();
  await page.getByRole("switch", { name: "批量提示词" }).click();
  // From the template page into a new smart request; the product link fills in.
  await page.getByRole("link", { name: "提示词模板" }).click();
  await page
    .locator(".object-row")
    .filter({ hasText: "链接模板" })
    .getByRole("button", { name: "用于智能生图" })
    .click();
  const instruction = page.getByLabel("完整制作指令", { exact: true });
  await expect(instruction).toHaveValue(
    "https://shop.example/item/9 做一套主图",
  );
  await expect(page.getByLabel("商品链接", { exact: true })).toHaveValue(
    "https://shop.example/item/9",
  );
  await page.screenshot({
    path: path.join(docs, "templates-smart-0.6.22.png"),
  });
  // On the smart page the same picker asks before replacing.
  await pick("产品摄影");
  await choose.getByRole("button", { name: "替换当前内容" }).click();
  await expect(instruction).toHaveValue("white background product");
  await instruction.fill("");
  await page.getByRole("link", { name: "提示词模板" }).click();
  await page.waitForTimeout(250);
  await page.screenshot({ path: path.join(docs, "templates-0.6.22.png") });
});
test("assets show images at once when coming back and keep readable names", async () => {
  await page.getByRole("link", { name: "素材库" }).click();
  await expect(page.getByRole("button", { name: /预览/ })).toHaveCount(1);
  await expect(page.getByRole("combobox", { name: "项目筛选" })).toHaveCount(0);
  await expect(page.locator(".asset-card-copy strong").first()).toHaveText(
    /^\d\d\/\d\d \d\d:\d\d$/,
  );
  await expect(page.locator(".asset-card img").first()).toBeVisible();
  // Saved images are read once: going away and back reads none again.
  await page.evaluate(() => {
    const app = window.go!.main!.App! as any;
    const read = app.ReadAsset;
    (window as any).__reads = 0;
    app.ReadAsset = (...a: unknown[]) => {
      (window as any).__reads++;
      return read(...a);
    };
  });
  await page.getByRole("link", { name: "任务中心" }).click();
  await expect(page.locator(".task-thumb img").first()).toBeVisible();
  await page.getByRole("combobox", { name: "任务状态" }).focus();
  await page.keyboard.press("ArrowDown");
  await expect(page.locator(".ant-select-dropdown:visible")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.getByRole("link", { name: "素材库" }).click();
  await expect(page.locator(".asset-card img").first()).toBeVisible();
  expect(await page.evaluate(() => (window as any).__reads)).toBe(0);
  await page.screenshot({
    path: path.join(docs, "desktop-assets.png"),
    fullPage: true,
  });
});
test("accessibility and small viewport layout", async () => {
  await page.getByRole("link", { name: "连接与设置" }).click();
  // A message fading out reads as low contrast; scan once it has gone.
  await expect(page.locator(".ant-message-notice")).toHaveCount(0, {
    timeout: 8000,
  });
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
test("素材库批量管理：多选、导出 ZIP 和移除", async () => {
  await page.getByRole("link", { name: "素材库" }).click();
  const go = (name: string, ...args: unknown[]) =>
    page.evaluate(
      ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
      [name, args] as const,
    );
  const total = async () =>
    ((await go("ListAssets", "", 0, 1)) as { total: number }).total;
  const before = await total();
  expect(before).toBeGreaterThan(3);
  await page.getByRole("button", { name: "批量管理", exact: true }).click();
  const bar = page.getByRole("toolbar", { name: "批量管理素材" });
  await expect(bar.getByText("已选 0 张")).toBeVisible();
  await expect(
    bar.getByRole("button", { name: "导出 ZIP", exact: true }),
  ).toBeDisabled();
  // In batch mode a card click selects instead of opening the preview.
  const picks = page.getByRole("button", { name: /^选择 / });
  await picks.nth(0).click();
  await picks.nth(1).press("Enter");
  await expect(picks.nth(0)).toHaveAttribute("aria-pressed", "true");
  await expect(bar.getByText("已选 2 张")).toBeVisible();
  await expect(page.getByRole("dialog", { name: "图片详情" })).toBeHidden();

  const output = fs.mkdtempSync(
    path.resolve(import.meta.dirname, "../../.tools/qa-assets-"),
  );
  const zipPath = path.join(output, "素材.zip");
  await page.evaluate((p) => {
    (window as any).originalExportPicker =
      window.go!.main!.App!.ChooseExportPath;
    window.go!.main!.App!.ChooseExportPath = async () => p;
  }, zipPath);
  await bar.getByRole("button", { name: "导出 ZIP", exact: true }).click();
  await expect(
    page.getByText("已导出素材 ZIP（2 张）", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    window.go!.main!.App!.ChooseExportPath = (
      window as any
    ).originalExportPicker;
  });
  const zip = fs.readFileSync(zipPath);
  let entries = 0;
  for (let i = 0; i < zip.length - 4; i++) {
    if (zip.readUInt32LE(i) === 0x02014b50) entries++;
  }
  expect(entries).toBe(2);

  await bar.getByRole("button", { name: /全选已加载/ }).click();
  await expect(
    bar.getByRole("button", { name: "取消全选", exact: true }),
  ).toBeVisible();
  await bar.getByRole("button", { name: "取消全选", exact: true }).click();
  await expect(bar.getByText("已选 0 张")).toBeVisible();
  await picks.nth(0).click();
  await picks.nth(1).click();
  await bar.getByRole("button", { name: "移除", exact: true }).click();
  const confirm = page.getByRole("dialog", {
    name: "从素材库移除 2 张图片？",
  });
  await confirm.getByRole("button", { name: "确认执行" }).click();
  await expect.poll(total).toBe(before - 2);
  // Scan only after the confirmation has finished fading out.
  await expect(confirm).toBeHidden();
  await expect(bar.getByText("已选 0 张")).toBeVisible();

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
    path: path.join(docs, "asset-batch-0.4.1.png"),
    fullPage: true,
  });
  await session.send("Emulation.clearDeviceMetricsOverride");
  await session.detach();
  await bar.getByRole("button", { name: "完成", exact: true }).click();
  await expect(bar).toBeHidden();
  await expect(
    page.getByRole("button", { name: /^预览 / }).first(),
  ).toBeVisible();
});
test("备用中转站：免费检查、主用/备用、结果未知自动重新生成与自动切换", async () => {
  const go = (name: string, ...args: unknown[]) =>
    page.evaluate(
      ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
      [name, args] as const,
    );
  const root = baseURL.replace("/custom/v1", "");
  const a = (await go("SaveConnection", {
    name: "QA 超时站",
    baseURL: `${root}/gw524/v1`,
    model: "qa-image",
    key: "qa-key-123",
  })) as { id: string };
  const b = (await go("SaveConnection", {
    name: "QA 备用站",
    baseURL: `${root}/gwok/v1`,
    model: "qa-image",
    key: "qa-key-123",
  })) as { id: string };
  await page.getByRole("link", { name: "连接与设置" }).click();
  const rowOf = (name: string) =>
    page.locator(".connection-row").filter({ hasText: name });
  await rowOf("QA 超时站").getByRole("radio", { name: "主用" }).click();
  await expect
    .poll(async () => ((await go("GetGatewaySettings")) as any).primaryID)
    .toBe(a.id);
  await rowOf("QA 备用站")
    .getByRole("switch", { name: "将 QA 备用站 设为备用中转站" })
    .click();
  await expect(rowOf("QA 备用站").getByText("备用 1")).toBeVisible();

  // The free check only reads model lists.
  const generationsBefore = gatewayHits.gw524 + gatewayHits.gwok;
  await page
    .getByRole("button", { name: "立即检查（免费）", exact: true })
    .click();
  await expect(
    page.getByText("免费检查完成（只读取模型列表，不产生费用）"),
  ).toBeVisible();
  expect(gatewayHits.models).toBeGreaterThanOrEqual(2);
  expect(gatewayHits.gw524 + gatewayHits.gwok).toBe(generationsBefore);
  await expect(
    rowOf("QA 备用站").getByText("正常", { exact: true }),
  ).toBeVisible();

  // Automatic re-generation is always on; the settings only explain its cost.
  await expect(
    page.getByRole("switch", { name: /524 后自动换中转站重新生成/ }),
  ).toHaveCount(0);
  await expect(
    page.getByText(/结果未知（例如 HTTP\s*524）的图片会自动重新生成一次/),
  ).toBeVisible();

  // Success notices fade out with low contrast and stay while hovered; move
  // away and wait for them (an earlier test's error notice stays open).
  await page.mouse.move(0, 0);
  await expect(page.locator(".ant-notification-notice-success")).toHaveCount(
    0,
    {
      timeout: 10000,
    },
  );
  const a11y = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);

  // Two images on the primary: the first is cut by 524 and re-generated once
  // on the backup; the second was never sent there and moves for free.
  await go("SetImageConcurrency", 1);
  await go("Submit", {
    requestID: crypto.randomUUID(),
    connectionID: a.id,
    model: "qa-image",
    prompts: ["gateway failover qa"],
    count: 2,
    references: [],
    size: "auto",
    quality: "auto",
    projectID: "",
  });
  await expect.poll(() => gatewayHits.gwok, { timeout: 20000 }).toBe(2);
  await page.waitForTimeout(500);
  expect(gatewayHits.gw524).toBe(1);
  expect(gatewayHits.gwok).toBe(2);
  await page.getByRole("link", { name: "生图工作台" }).click();
  const latest = page.locator(".recent-batch").first();
  await expect(
    latest.getByText(/结果未知后自动重新生成 · 「QA 备用站」/),
  ).toBeVisible({
    timeout: 15000,
  });
  await expect(
    latest.getByText(/已从「QA 超时站」转到「QA 备用站」/),
  ).toBeVisible();
  // The cut image keeps a short note instead of a warning.
  await expect(
    latest.getByText("结果未知，已自动重新生成（原请求可能已计费）。"),
  ).toBeVisible();
  await expect(latest.locator(".error-text")).toHaveCount(0);
  const session = await browser.contexts()[0].newCDPSession(page);
  await session.send("Emulation.setDeviceMetricsOverride", {
    width: 900,
    height: 740,
    deviceScaleFactor: 1,
    mobile: false,
  });
  await page.getByRole("link", { name: "连接与设置" }).click();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth,
    ),
  ).toBe(false);
  await page.screenshot({
    path: path.join(docs, "gateway-failover-0.5.0.png"),
    fullPage: true,
  });
  await session.send("Emulation.clearDeviceMetricsOverride");
  await session.detach();
  await go("SetGatewaySettings", {
    primaryID: "",
    backupIDs: [],
    autoReroute: true,
  });
  await go("SetImageConcurrency", 3);
  expect(b.id).toBeTruthy();
});
test("中转站监控：按中转站统计、调用记录筛选与导出 CSV", async () => {
  await page.getByRole("link", { name: "中转站监控" }).click();
  await expect(
    page.getByRole("heading", { name: "中转站监控", level: 1 }),
  ).toBeVisible();
  const card = (name: string) =>
    page.locator(".monitor-card").filter({ hasText: name });
  // From the failover test: one 524 on the timeout gateway; the backup got the
  // moved image and the automatic re-generation.
  await expect(card("QA 超时站").locator(".monitor-big strong")).toHaveText(
    "1",
  );
  await expect(card("QA 备用站").locator(".monitor-big strong")).toHaveText(
    "2",
  );
  await expect(card("QA 超时站")).toContainText("转出 1 张到备用");
  await expect(card("QA 备用站")).toContainText("转入 1 张");
  await expect(card("QA 备用站")).toContainText("自动重新生成 1 张");
  await expect(page.getByText("从 QA 超时站 转入").first()).toBeVisible();
  await page
    .getByRole("combobox", { name: "调用结果" })
    .locator("..")
    .locator("..")
    .click();
  await page
    .locator(
      '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option[title="结果未知"]',
    )
    .click();
  const rows = page.locator(".ant-table-tbody tr.ant-table-row");
  await expect(rows.filter({ hasText: "QA 超时站" }).first()).toContainText(
    "结果未知 · HTTP 524",
  );
  await expect(rows.filter({ hasText: "成功" })).toHaveCount(0);

  const output = fs.mkdtempSync(
    path.resolve(import.meta.dirname, "../../.tools/qa-monitor-"),
  );
  const csvPath = path.join(output, "调用记录.csv");
  await page.evaluate((p) => {
    (window as any).originalExportPicker =
      window.go!.main!.App!.ChooseExportPath;
    window.go!.main!.App!.ChooseExportPath = async () => p;
  }, csvPath);
  await page.getByRole("button", { name: "导出 CSV" }).click();
  await expect(page.getByText(/已导出 \d+ 条调用记录/)).toBeVisible();
  await page.evaluate(() => {
    window.go!.main!.App!.ChooseExportPath = (
      window as any
    ).originalExportPicker;
  });
  const csv = fs.readFileSync(csvPath, "utf8");
  expect(csv.startsWith("﻿时间,中转站")).toBe(true);
  expect(csv).toContain("QA 超时站");
  expect(csv).not.toContain("QA 备用站"); // the result filter applies to the export
  expect(csv).not.toContain("qa-key-123");

  await expect(
    page.locator(".ant-select-dropdown:not(.ant-select-dropdown-hidden)"),
  ).toHaveCount(0);
  // A notification fading out reads as low contrast; scan once they have gone.
  // Hovering pauses their timer, so the cursor moves away first.
  await page.mouse.move(0, 0);
  await expect(page.locator(".ant-notification-notice-success")).toHaveCount(
    0,
    {
      timeout: 8000,
    },
  );
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
    path: path.join(docs, "gateway-monitor-0.5.4.png"),
    fullPage: true,
  });
  await session.send("Emulation.clearDeviceMetricsOverride");
  await session.detach();
});
