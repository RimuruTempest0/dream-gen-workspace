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
import { inflateSync } from "node:zlib";
import AxeBuilder from "@axe-core/playwright";

let browser: Browser, page: Page, server: http.Server, baseURL: string;
let imageRequests = 0,
  modelRequests = 0,
  analysisRequests = 0;
const image = fs.readFileSync(path.resolve("tests/fixture.png"));
const flows = new Map<string, number>();
function imageColor(data: string) {
  const png = Buffer.from(data, "base64");
  const chunks: Buffer[] = [];
  for (let p = 8; p < png.length; ) {
    const len = png.readUInt32BE(p);
    if (png.toString("ascii", p + 4, p + 8) === "IDAT")
      chunks.push(png.subarray(p + 8, p + 8 + len));
    p += 12 + len;
  }
  const pixels = inflateSync(Buffer.concat(chunks));
  return pixels[1] > pixels[3] ? "RED" : "BLUE";
}
function firstJSON(text: string) {
  let quoted = false,
    escaped = false,
    depth = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (quoted) {
      if (escaped) escaped = false;
      else if (c === "\\") escaped = true;
      else if (c === '"') quoted = false;
    } else if (c === '"') quoted = true;
    else if (c === "{") depth++;
    else if (c === "}" && --depth === 0)
      return JSON.parse(text.slice(0, i + 1));
  }
  throw new Error("context JSON not found");
}
function emit(res: http.ServerResponse, block: any, id: string) {
  res.setHeader("content-type", "text/event-stream");
  const frame = (v: any) =>
    res.write(`event: ${v.type}\ndata: ${JSON.stringify(v)}\n\n`);
  frame({
    type: "message_start",
    message: {
      id: "msg_mock",
      type: "message",
      role: "assistant",
      model: "mock-agent",
      content: [],
      stop_reason: null,
      stop_sequence: null,
      usage: { input_tokens: 100, output_tokens: 0 },
    },
  });
  const tool = block.type === "tool_use";
  frame({
    type: "content_block_start",
    index: 0,
    content_block: tool
      ? { type: "tool_use", id, name: block.name, input: {} }
      : { type: "text", text: "" },
  });
  frame({
    type: "content_block_delta",
    index: 0,
    delta: tool
      ? { type: "input_json_delta", partial_json: JSON.stringify(block.input) }
      : { type: "text_delta", text: block.text },
  });
  frame({ type: "content_block_stop", index: 0 });
  frame({
    type: "message_delta",
    delta: { stop_reason: tool ? "tool_use" : "end_turn", stop_sequence: null },
    usage: { output_tokens: 100 },
  });
  frame({ type: "message_stop" });
  res.end();
}
async function go<T>(name: string, ...args: any[]): Promise<T> {
  return page.evaluate(
    async ({ name, args }) => {
      const fn = window.go?.main?.App?.[name];
      if (!fn) throw new Error("missing binding " + name);
      return fn(...args);
    },
    { name, args },
  ) as Promise<T>;
}

// Mock shop: the first visit is a human-check page that the in-app browser
// "passes" after 12 s (cookie + reload), so the verification window is shown.
const shopTitle = "QA 浏览器商品";
let shopPhotoRequests = 0;
function shopPage(req: http.IncomingMessage, res: http.ServerResponse) {
  res.setHeader("content-type", "text/html; charset=utf-8");
  if (!req.headers.cookie?.includes("qa_passed=1")) {
    res.end(`<html><body><p>Please enable JS and disable any ad blocker</p><script>
setTimeout(function(){ document.cookie = "qa_passed=1; path=/"; location.replace(location.pathname + "?dd_referrer=qa"); }, 12000);
</script></body></html>`);
    return;
  }
  const product = {
    "@type": "Product",
    name: shopTitle,
    description:
      "页面脚本渲染的完整商品描述：纯棉刺绣卫衣，尺寸与颜色以商品页为准。",
    image: [{ "@type": "ImageObject", contentURL: "/qa-shop/photo.png" }],
  };
  res.end(`<html><head></head><body><p>商品</p><script>
var s = document.createElement("script"); s.type = "application/ld+json";
s.textContent = ${JSON.stringify(JSON.stringify(product))};
document.head.appendChild(s);
</script></body></html>`);
}

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    if (req.url?.startsWith("/qa-shop/product")) return shopPage(req, res);
    if (req.url === "/qa-shop/photo.png") {
      shopPhotoRequests++;
      res.setHeader("content-type", "image/png");
      res.end(image);
      return;
    }
    if (req.url?.endsWith("/models")) {
      modelRequests++;
      res.setHeader("content-type", "application/json");
      res.end(
        JSON.stringify({ data: [{ id: "mock-agent" }, { id: "mock-image" }] }),
      );
      return;
    }
    if (req.url?.includes("/images/")) {
      imageRequests++;
      req.resume();
      req.on("end", () => {
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({ data: [{ b64_json: image.toString("base64") }] }),
        );
      });
      return;
    }
    if (!req.url?.endsWith("/messages")) {
      res.writeHead(404);
      res.end();
      return;
    }
    let raw = "";
    req.on("data", (c) => (raw += c));
    req.on("end", () => {
      const body = JSON.parse(raw);
      const first = body.messages[0].content;
      if (typeof first === "string" && first === "Reply exactly OK.") {
        if (body.stream) emit(res, { type: "text", text: "OK" }, "");
        else {
          res.setHeader("content-type", "application/json");
          res.end(JSON.stringify({ content: [{ type: "text", text: "OK" }] }));
        }
        return;
      }
      if (Array.isArray(first) && first[0]?.type === "image") {
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({
            content: [{ type: "text", text: imageColor(first[0].source.data) }],
          }),
        );
        return;
      }
      if (
        typeof first === "string" &&
        first.startsWith("Call capability_echo with nonce ")
      ) {
        if (body.tool_choice?.type !== "auto") {
          res.writeHead(400, { "content-type": "application/json" });
          res.end(
            JSON.stringify({
              error: {
                message: "Thinking mode does not support this tool_choice",
              },
            }),
          );
          return;
        }
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({
            content: [
              {
                type: "tool_use",
                name: "capability_echo",
                input: {
                  nonce: first.replace("Call capability_echo with nonce ", ""),
                },
              },
            ],
          }),
        );
        return;
      }
      analysisRequests++;
      const text =
        body.messages
          .flatMap((m: any) =>
            typeof m.content === "string"
              ? [m.content]
              : m.content
                  ?.filter((b: any) => b.type === "text")
                  .map((b: any) => b.text) || [],
          )
          .find((v: string) => v.includes("以下为规范化需求上下文")) || "";
      const marker = text.indexOf("以下为规范化需求上下文");
      const ctx = firstJSON(text.slice(text.indexOf("{", marker)));
      const answered = ctx.questions?.some((q: any) => q.answer);
      const key = ctx.id + ":" + answered;
      const step = flows.get(key) || 0;
      flows.set(key, step + 1);
      const tool = (name: string, input: any) => ({
        type: "tool_use",
        name,
        input,
      });
      let block: any;
      if (ctx.input.productURL?.includes("/qa-shop/")) {
        block =
          step === 0
            ? tool("mcp__workbench__get_run_context", {})
            : step === 1
              ? tool("mcp__workbench__fetch_product", {})
              : step === 2
                ? tool("mcp__workbench__request_information", {
                    questions: [
                      { id: "size", label: "请补充真实尺寸", required: true },
                    ],
                  })
                : { type: "text", text: "请补充真实尺寸。" };
        emit(res, block, `tool_${step}`);
        return;
      }
      if (step === 0) block = tool("mcp__workbench__get_run_context", {});
      else if (!answered && step === 1)
        block = tool("Skill", { skill: "etsy-suite" });
      else if (!answered && step === 2)
        block = tool("mcp__workbench__request_information", {
          questions: [
            {
              id: "p004",
              label: "请补充 P004 真实客户成品机绣规则",
              required: true,
            },
          ],
        });
      else if (answered && step === 1)
        block = tool("mcp__workbench__read_reference_image", {
          assetID: ctx.input.references[0],
        });
      else if (answered && step === 2)
        block = tool("mcp__workbench__save_suite_plan", {
          locked: ["衣服造型和比例", "MM 字体"],
          editable: ["背景", "名字"],
          notes: "12张 Etsy 1:1 套图",
          items: Array.from({ length: 12 }, (_, i) => ({
            id: `slot-${i + 1}`,
            purpose: `展示图 ${i + 1}`,
            scene: i === 5 ? "情侣卧室" : "家庭客厅",
            prompt: `小金毛刺绣卫衣，第 ${i + 1} 张，名字清晰，真实商品比例。`,
            references: ctx.input.references.slice(0, 1),
            size: "1024x1024",
            quality: "auto",
          })),
        });
      else
        block = {
          type: "text",
          text: answered
            ? "已保存12张方案，等待确认生图。"
            : "请补充 P004 规则。",
        };
      emit(res, block, `tool_${step}`);
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  baseURL = `http://127.0.0.1:${(server.address() as any).port}/prefix/v1`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  await go("ResetDesktopQA");
  await page.goto("http://wails.localhost/#/settings");
});
test.afterAll(async () => {
  if (page) {
    await page.goto("http://wails.localhost/#/");
  }
  await browser?.close();
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

test("LLM settings use separate model lookup and validated agent capabilities", async () => {
  await expect(page.getByRole("heading", { name: "生图中转站" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "LLM 供应商" })).toBeVisible();
  await expect(page.getByText("内置运行时已就绪")).toBeVisible();
  await page.getByRole("button", { name: "添加 LLM 供应商" }).click();
  const modal = page.getByRole("dialog");
  await modal.getByLabel("连接名称", { exact: true }).fill("Agent QA LLM");
  await modal.getByLabel("Base URL", { exact: true }).fill(baseURL);
  await modal.getByLabel("API Key", { exact: true }).fill("mock-llm-key");
  await expect(modal.getByLabel("API Key", { exact: true })).toHaveAttribute(
    "type",
    "password",
  );
  await modal.getByRole("button", { name: "获取模型", exact: true }).click();
  await expect(modal.getByText("模型查询认证通过")).toBeVisible();
  expect(modelRequests).toBe(1);
  expect(analysisRequests).toBe(0);
  expect(imageRequests).toBe(0);
  await modal.getByLabel("LLM 模型 ID", { exact: true }).fill("mock-agent");
  await modal
    .getByLabel("模型模式", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await page.getByText("其他模型 · 兼容接入", { exact: true }).click();
  await modal.getByRole("button", { name: "保存连接", exact: true }).click();
  await expect(modal).not.toBeVisible();
  const row = page
    .locator(".connection-row")
    .filter({ hasText: "Agent QA LLM" });
  await row.getByRole("button", { name: "验证 Agent 能力" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "开始验证", exact: true })
    .click();
  await expect(row.getByText("图片 · 通过", { exact: true })).toBeVisible({
    timeout: 15000,
  });
  await expect(row.getByText("工具 · 通过", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "添加连接", exact: true }).click();
  const imageModal = page.getByRole("dialog");
  await imageModal
    .getByLabel("连接名称", { exact: true })
    .fill("Agent QA 生图");
  await imageModal.getByLabel("Base URL", { exact: true }).fill(baseURL);
  await imageModal
    .getByLabel("API Key", { exact: true })
    .fill("mock-image-key");
  await imageModal.getByLabel("默认模型 ID（可选）").fill("mock-image");
  await imageModal
    .getByRole("button", { name: "保存连接", exact: true })
    .click();
  await expect(imageModal).not.toBeVisible();
  await page.getByRole("link", { name: "生图工作台", exact: true }).click();
  await page.getByLabel("中转站连接", { exact: true }).first().click();
  await expect(
    page.getByText("Agent QA LLM", { exact: true }),
  ).not.toBeVisible();
  await page.keyboard.press("Escape");
});

test("instruction links auto-fill once and multiple links require a choice", async () => {
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
  const instruction = page.getByLabel("完整制作指令", { exact: true });
  const url = page.getByLabel("商品链接", { exact: true });
  await instruction.fill(
    "Etsy 套图 [(https://example.com/product-one)\\]，12张，1:1",
  );
  await expect(url).toHaveValue("https://example.com/product-one");
  await instruction.fill(
    "https://example.com/product-one https://example.com/product-two",
  );
  await expect(url).toHaveValue("");
  await expect(
    page.getByLabel("选择本次商品链接", { exact: true }),
  ).toBeVisible();
  await url.fill("https://example.com/manually-selected");
  await instruction.fill("https://example.com/product-one");
  await expect(url).toHaveValue("https://example.com/manually-selected");
  await url.fill("");
  await instruction.fill("");
  await expect
    .poll(async () => (await go<any>("GetAgentDraft")).instruction)
    .toBe("");
});

test("native agent requests necessary materials and automatically generates twelve images", async () => {
  test.setTimeout(90000);
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
  await page
    .getByLabel("完整制作指令", { exact: true })
    .fill(
      "Etsy 至少12张套图：小金毛刺绣卫衣，MM 字体锁定，执行 P004 真实客户机绣。第6张情侣卧室，1:1。",
    );
  await page.getByText("更多设置", { exact: true }).click();
  await page
    .getByLabel("LLM 供应商", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await page.getByText("Agent QA LLM", { exact: true }).last().click();
  await page
    .getByLabel("生图中转站", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await page.getByText("Agent QA 生图", { exact: true }).last().click();
  await expect
    .poll(async () => (await go<any>("GetAgentDraft")).imageConnectionID)
    .not.toBe("");
  // Every field in 更多设置 is a dropdown; nothing is typed by hand.
  const settings = page.locator(".ant-collapse-content");
  await expect(settings.locator('input:not([role="combobox"])')).toHaveCount(0);
  await expect(settings.locator("textarea")).toHaveCount(0);
  await expect(page.getByText("使用连接默认（mock-image）")).toBeVisible();
  await page
    .getByLabel("生图模型", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await expect(
    page.locator('.ant-select-item-option[title="mock-image"]'),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await page
    .getByLabel("图片尺寸", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await expect(page.getByText("1536×1024（3:2 横图）")).toBeVisible();
  await page.keyboard.press("Escape");
  expect(await go<number>("ImageConcurrency")).toBe(3);
  await page
    .getByLabel("同时生成张数", { exact: true })
    .locator("..")
    .locator("..")
    .click();
  await page.locator('.ant-select-item-option[title="2 张"]').click();
  await expect.poll(() => go<number>("ImageConcurrency")).toBe(2);
  const ref = await go<{ id: string }>(
    "ImportImage",
    "qa-product.png",
    image.toString("base64"),
  );
  await page.evaluate(async (id) => {
    const get = window.go!.main!.App!.GetAgentDraft;
    const set = window.go!.main!.App!.SetAgentDraft;
    const draft = await get();
    await set({ ...draft, references: [id] });
  }, ref.id);
  // Reload the persisted draft so the input and the backend share the actual imported reference.
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect(page.getByLabel("完整制作指令", { exact: true })).toHaveValue(
    /Etsy/,
  );
  await page.getByRole("button", { name: "开始生成套图", exact: true }).click();
  await expect(page.getByText("等待补充", { exact: true }).first()).toBeVisible(
    { timeout: 30000 },
  );
  expect(imageRequests).toBe(0);
  await page
    .getByLabel("请补充 P004 真实客户成品机绣规则")
    .fill(
      "P004：真实客户机绣，小金毛头像在 MM 中间，袖口黑色脚掌，按参考图比例。",
    );
  await page
    .getByRole("button", { name: "补充并继续制作", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "分析与规划", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByRole("heading", { name: /套图方案/ })).toHaveCount(0);
  await expect(page.locator(".suite-item")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /确认生成/ })).toHaveCount(0);
  await expect(
    page.getByText("已完成 12 / 12 个任务，可下载 12 张图片", { exact: true }),
  ).toBeVisible({ timeout: 30000 });
  expect(imageRequests).toBe(12);
  await expect(
    page.getByRole("button", { name: "下载整套图片", exact: true }),
  ).toBeEnabled();
  // Parallelism stays adjustable after the set is created; other fields are locked.
  await page.getByText("更多设置", { exact: true }).click();
  await expect(page.getByLabel("同时生成张数", { exact: true })).toBeEnabled();
  await expect(page.getByLabel("图片尺寸", { exact: true })).toBeDisabled();
  // Collapse again and let the animation finish so axe sees final colors.
  await page.getByText("更多设置", { exact: true }).click();
  await expect(page.getByLabel("同时生成张数", { exact: true })).toBeHidden();
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
  await cdp.send("Emulation.clearDeviceMetricsOverride");
  await cdp.detach();
  await page.screenshot({
    path: "../docs/smart-images-0.3.0.png",
    fullPage: true,
  });
  await page.getByRole("link", { name: "任务中心", exact: true }).click();
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
  await page
    .getByRole("button", { name: "打开记录", exact: true })
    .first()
    .click();
  await expect(
    page.getByText("已完成 12 / 12 个任务，可下载 12 张图片", { exact: true }),
  ).toBeVisible();
});

test("refine a finished image keeps the previous version and stays off the workbench", async () => {
  const before = imageRequests;
  const first = page.locator(".agent-result").first();
  await first.getByRole("button", { name: "优化第 1 张图片" }).click();
  const dialog = page.getByRole("dialog", { name: "优化这张图片" });
  await expect(dialog.getByText("可能产生费用")).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "开始优化", exact: true }),
  ).toBeDisabled();
  await dialog
    .getByLabel("想怎么调整？")
    .fill("袖口名字改成横排，与其他图片一致");
  await dialog.getByRole("button", { name: "开始优化", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(
    first.getByText("优化：袖口名字改成横排，与其他图片一致"),
  ).toBeVisible({ timeout: 20000 });
  await expect(first.getByText("之前的版本（1）")).toBeVisible();
  await expect(
    first.getByRole("button", { name: "优化第 1 张图片" }),
  ).toBeVisible({ timeout: 20000 });
  expect(imageRequests).toBe(before + 1);
  await expect(
    page.getByText("已完成 12 / 12 个任务，可下载 12 张图片", { exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "生图工作台", exact: true }).click();
  await expect(page.getByText("第一张图片，从这里开始")).toBeVisible();
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
});

test("set images open in a large viewer with keyboard and button navigation", async () => {
  await page
    .getByRole("button", { name: "打开记录", exact: true })
    .first()
    .click();
  await page.getByRole("button", { name: "查看第 1 张大图" }).click();
  const viewer = page.getByRole("dialog", { name: "第 1 张（1 / 13）" });
  await expect(viewer).toBeVisible();
  await expect(viewer.getByRole("img", { name: "第 1 张" })).toBeVisible();
  await expect(
    viewer.getByRole("button", { name: "上一张", exact: true }),
  ).toBeDisabled();
  await expect(
    viewer.getByRole("button", { name: "优化", exact: true }),
  ).toBeVisible();
  await page.keyboard.press("ArrowRight");
  const previous = page.getByRole("dialog", {
    name: "第 1 张 · 之前的版本 1（2 / 13）",
  });
  await expect(previous).toBeVisible();
  await expect(
    previous.getByRole("button", { name: "优化", exact: true }),
  ).toHaveCount(0);
  await previous.getByRole("button", { name: "下一张", exact: true }).click();
  const second = page.getByRole("dialog", { name: "第 2 张（3 / 13）" });
  await expect(second).toBeVisible();
  await expect(
    second.getByRole("button", { name: "下载原图", exact: true }),
  ).toBeVisible();
  await second.getByRole("img", { name: "第 2 张" }).click();
  await expect(second.locator(".image-viewer.actual")).toHaveCount(1);
  await page.keyboard.press("Escape");
  await expect(second).toHaveCount(0);
});

test("blocked product link waits for browser verification and continues", async () => {
  test.setTimeout(120000);
  const before = imageRequests;
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
  const shopURL = baseURL.replace("/prefix/v1", "/qa-shop/product");
  await page.evaluate(async (productURL) => {
    const app = window.go!.main!.App!;
    const draft = await app.GetAgentDraft();
    await app.SetAgentDraft({
      ...draft,
      productURL,
      instruction: "Etsy 套图：读取商品链接后制作，12张，1:1。",
      references: [],
    });
  }, shopURL);
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect(page.getByLabel("商品链接", { exact: true })).toHaveValue(
    shopURL,
  );
  await page.getByRole("button", { name: "开始生成套图", exact: true }).click();
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: "需要你在弹出的窗口里完成验证" }),
  ).toBeVisible({ timeout: 40000 });
  const waiting = page.getByText(/等待验证中（已 \d+ 秒）/);
  await expect(waiting).toBeVisible();
  const first = Number(
    (await waiting.textContent())?.match(/已 (\d+) 秒/)?.[1],
  );
  await expect
    .poll(async () =>
      Number((await waiting.textContent())?.match(/已 (\d+) 秒/)?.[1]),
    )
    .toBeGreaterThan(first);
  await page.screenshot({
    path: "../docs/smart-images-verify-0.3.1.png",
    fullPage: true,
  });
  await expect(page.getByText("等待补充", { exact: true }).first()).toBeVisible(
    { timeout: 40000 },
  );
  await expect(waiting).toHaveCount(0);
  const runs = await go<{ items: any[] }>("ListAgentRuns", 0, 50);
  const run = runs.items.find((r) => r.input.productURL === shopURL);
  expect(run.product.title).toBe(shopTitle);
  expect(run.product.error).toBe("");
  expect(run.product.referenceIDs).toHaveLength(1);
  expect(shopPhotoRequests).toBeGreaterThan(0);
  expect(imageRequests).toBe(before);
  const logs = await go<{ items: { message: string }[] }>(
    "ListLogs",
    "",
    "agent",
    "",
    0,
    50,
  );
  const text = logs.items.map((l) => l.message).join("\n");
  expect(text).toContain("改用浏览器读取");
  expect(text).toContain("已弹出验证窗口");
  expect(text).toContain("浏览器读取商品页完成");
  expect(text).not.toContain("dd_referrer");
  // Leave a blank draft so a rerun of the link auto-fill test starts clean.
  await page.evaluate(async () => {
    const app = window.go!.main!.App!;
    await app.SetAgentDraft({
      ...(await app.GetAgentDraft()),
      productURL: "",
      instruction: "",
    });
  });
});

test("settings clear product browser data after confirmation", async () => {
  await page.getByRole("link", { name: "连接与设置", exact: true }).click();
  const button = page.getByRole("button", {
    name: "清除商品页浏览器数据",
    exact: true,
  });
  await button.click();
  const dialog = page.getByRole("dialog", { name: "清除商品页浏览器数据？" });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await button.click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "清除", exact: true })
    .click();
  await expect(page.getByText("已清除商品页浏览器数据")).toBeVisible();
});

test("Skills creation, enable switch and editing preserve local rules", async () => {
  await page.getByRole("link", { name: "连接与设置", exact: true }).click();
  await page.getByRole("button", { name: "新建 Skill", exact: true }).click();
  const modal = page.getByRole("dialog");
  await modal.getByLabel("技能名称", { exact: true }).fill("QA 定制规则");
  await modal
    .getByLabel("SKILL.md", { exact: true })
    .fill(
      "---\nname: qa-rules\ndescription: 真实商品比例规则\n---\n衣服比例和名字必须保持。",
    );
  await modal.getByRole("button", { name: "保存 Skill", exact: true }).click();
  await expect(modal).not.toBeVisible();
  const row = page
    .locator(".connection-row")
    .filter({ hasText: "QA 定制规则" });
  await expect(row).toBeVisible();
  await row.getByRole("switch").click();
  await expect(row.getByRole("switch")).toHaveAttribute(
    "aria-checked",
    "false",
  );
  await page.screenshot({
    path: "../docs/agent-settings-0.3.0.png",
    fullPage: true,
  });
});
