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
// While set, the mock holds its reply that saves the plan, to look at the steps.
let planGate: Promise<void> | null = null;
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
let rejectImageKey = false;
// The image relay refuses with 403 because the account has no balance (1route-style).
let emptyImageAccount = false;
// Cut the next answered analysis stream after its first frame, like a proxy
// dropping the LLM connection; Claude Code should retry and finish.
let cutStreamOnce = false;
let returnImageURL = false;
let resultLinkUnavailable = false;
// The next image requests are refused by the image model's content check.
// Set when an automatic-mode request offered the self-answer tool.
let autoOfferedAnswers = false;
let refuseImages = 0,
  refusedRequests = 0,
  rewriteRequests = 0,
  // Checks of the detail sample; the first one fails so the sample is redrawn.
  sampleChecks = 0,
  // Refinements whose original requirements the LLM rewrote first.
  refineRewrites = 0;
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
    // The second "image" is not a picture: the page is read, one image is skipped.
    image: [
      { "@type": "ImageObject", contentURL: "/qa-shop/photo.png" },
      { "@type": "ImageObject", contentURL: "/qa-shop/broken.png" },
    ],
  };
  res.end(`<html><head></head><body><p>商品</p><script>
var s = document.createElement("script"); s.type = "application/ld+json";
s.textContent = ${JSON.stringify(JSON.stringify(product))};
document.head.appendChild(s);
</script></body></html>`);
}

test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    if (req.url === "/qa-result.png") {
      if (resultLinkUnavailable) {
        res.writeHead(404);
        res.end();
      } else {
        res.setHeader("content-type", "image/png");
        res.end(image);
      }
      return;
    }
    if (req.url?.startsWith("/qa-shop/product")) return shopPage(req, res);
    if (req.url === "/qa-shop/photo.png") {
      shopPhotoRequests++;
      res.setHeader("content-type", "image/png");
      res.end(image);
      return;
    }
    if (req.url === "/qa-shop/broken.png") {
      res.setHeader("content-type", "image/png");
      res.end("not an image");
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
    if (req.url?.includes("/images/") && emptyImageAccount) {
      req.resume();
      res.writeHead(403, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: { message: "insufficient balance" } }));
      return;
    }
    if (req.url?.includes("/images/") && refuseImages > 0) {
      refuseImages--;
      refusedRequests++;
      req.resume();
      res.writeHead(400, { "content-type": "application/json" });
      res.end(
        JSON.stringify({
          error: {
            message:
              "非常抱歉，该提示可能违反了我们的内容政策。如果你认为此判断有误，请重试或修改提示语。",
          },
        }),
      );
      return;
    }
    if (req.url?.includes("/images/") && rejectImageKey) {
      req.resume();
      res.writeHead(401, { "content-type": "application/json" });
      res.end(JSON.stringify({ error: { message: "Invalid API key" } }));
      return;
    }
    if (req.url?.includes("/images/")) {
      imageRequests++;
      req.resume();
      req.on("end", () => {
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({
            data: [
              returnImageURL
                ? { url: new URL("/qa-result.png", baseURL).href }
                : { b64_json: image.toString("base64") },
            ],
          }),
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
      if (
        typeof first === "string" &&
        first.startsWith("图片模型的内容安全审核拒绝了")
      ) {
        rewriteRequests++;
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({
            content: [
              {
                type: "text",
                text: "改写后：" + first.split("原提示词：\n")[1],
              },
            ],
            usage: { input_tokens: 400, output_tokens: 200 },
          }),
        );
        return;
      }
      if (
        typeof first === "string" &&
        first.startsWith("下面是一张电商图片原来的制作要求")
      ) {
        refineRewrites++;
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({
            content: [{ type: "text", text: first.split("原制作要求：\n")[1] }],
            usage: { input_tokens: 300, output_tokens: 150 },
          }),
        );
        return;
      }
      if (
        Array.isArray(first) &&
        first[0]?.type === "text" &&
        first[0].text.startsWith("你是刺绣商品图的质检员")
      ) {
        sampleChecks++;
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({
            content: [
              {
                type: "text",
                text:
                  sampleChecks === 1
                    ? '{"pass": false, "problems": ["头饰画成了圣诞帽，应是迷你圣诞袜"]}'
                    : '{"pass": true, "problems": []}',
              },
            ],
          }),
        );
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
      // Claude Code may retry a dropped stream without streaming.
      const reply = (block: any, id: string) => {
        if (body.stream) return emit(res, block, id);
        const tool = block.type === "tool_use";
        res.setHeader("content-type", "application/json");
        res.end(
          JSON.stringify({
            id: "msg_mock",
            type: "message",
            role: "assistant",
            model: "mock-agent",
            content: [tool ? { ...block, id } : block],
            stop_reason: tool ? "tool_use" : "end_turn",
            stop_sequence: null,
            usage: { input_tokens: 100, output_tokens: 100 },
          }),
        );
      };
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
      // An answer that does not settle the question: it is asked again.
      const vague = ctx.questions?.some((q: any) => q.answer === "上传了");
      const key = ctx.id + ":" + answered + ":" + vague;
      const step = flows.get(key) || 0;
      if (cutStreamOnce && answered) {
        cutStreamOnce = false;
        // The stream opens, then the connection drops before the first event,
        // as when the local proxy closed the LLM connection while waiting.
        res.setHeader("content-type", "text/event-stream");
        res.flushHeaders();
        setTimeout(() => res.destroy(), 50);
        return;
      }
      flows.set(key, step + 1);
      const tool = (name: string, input: any) => ({
        type: "tool_use",
        name,
        input,
      });
      let block: any;
      if (vague) {
        block =
          step === 0
            ? tool("mcp__workbench__get_run_context", {})
            : step === 1
              ? tool("mcp__workbench__request_information", {
                  questions: [
                    {
                      id: "p004",
                      label: "这个回答没有说明规则，请写出 P004 机绣规则",
                      required: true,
                    },
                  ],
                })
              : { type: "text", text: "请写出 P004 机绣规则。" };
        reply(block, `tool_${step}`);
        return;
      }
      if (ctx.input.autoMode) {
        // Automatic mode: ask the screenshot's four questions, answer them
        // itself the way the user would, then save the plan.
        if (
          body.tools?.some((t: any) =>
            String(t.name).endsWith("answer_questions"),
          )
        )
          autoOfferedAnswers = true;
        const ask = (id: string, label: string) => ({
          id,
          label,
          required: true,
        });
        const answer = (
          id: string,
          a: string,
          basis: string,
          reason: string,
        ) => ({
          id,
          answer: a,
          basis,
          reason,
        });
        block =
          step === 0
            ? tool("mcp__workbench__request_information", {
                questions: [
                  ask("pet", "新宠物肖像的品种和毛色"),
                  ask("name", "绣在袜口上的新英文名字"),
                  ask("thread", "新的名字绣线颜色"),
                  ask("hat", "保留的圣诞帽是参考图里红绿迷你圣诞袜帽这一款吗"),
                ],
              })
            : step === 1
              ? tool("mcp__workbench__answer_questions", {
                  answers: [
                    answer("pet", "金毛", "instruction", "指令写了小金毛"),
                    answer(
                      "name",
                      "BELLA",
                      "verify",
                      "指令没给名字，取一个常见名字",
                    ),
                    answer("thread", "白色", "choice", "白色在墨绿底上最清楚"),
                    answer(
                      "hat",
                      "原链接里的红绿迷你圣诞袜帽",
                      "source",
                      "指令说保留，照原链接",
                    ),
                  ],
                })
              : step === 2
                ? tool("mcp__workbench__save_suite_plan", {
                    locked: ["衣服造型和比例", "胸前名字 Bella（自动决定）"],
                    editable: ["背景"],
                    notes: "自动决定：名字 Bella、线色白色。12张 Etsy 1:1 套图",
                    cuffEmbroidery: false,
                    items: Array.from({ length: 12 }, (_, i) => ({
                      id: `auto-${i + 1}`,
                      purpose: `自动展示图 ${i + 1}`,
                      scene: "家庭客厅",
                      prompt: `小金毛刺绣卫衣，自动模式第 ${i + 1} 张，真实商品比例。`,
                      references: [],
                      // 11 is the cuff sample, 12 the chest master; the rest copy both.
                      sample: i >= 10,
                      sampleArea:
                        i === 10 ? "袖口" : i === 11 ? "胸前" : undefined,
                      useSamples: i < 10 ? ["胸前", "袖口"] : undefined,
                      size: "1024x1024",
                      quality: "auto",
                    })),
                  })
                : {
                    type: "text",
                    text: "已保存12张方案（含自行判断的取值）。",
                  };
        reply(block, `tool_${step}`);
        return;
      }
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
        reply(block, `tool_${step}`);
        return;
      }
      if (step === 0) block = tool("mcp__workbench__get_run_context", {});
      else if (!answered && step === 1)
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
          // The instruction puts a black paw on the cuff (required with P004 since 0.5.2).
          cuffEmbroidery: true,
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
      if (answered && step === 2 && planGate) {
        planGate.then(() => reply(block, `tool_${step}`));
        return;
      }
      reply(block, `tool_${step}`);
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
  // The backend fills a lone connection itself, so also wait for the typed instruction.
  await expect
    .poll(async () => (await go<any>("GetAgentDraft")).instruction)
    .toMatch(/Etsy/);
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
  await expect(
    page.locator(
      ".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option",
    ),
  ).toHaveText(["1 张（逐张生成）", "2 张", "3 张", "4 张"]);
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
  // The asset picker shows a thumbnail beside each name; hovering it enlarges it.
  await page.getByRole("combobox", { name: "从素材库选择参考图" }).click();
  const pickerOption = page
    .locator(".asset-option")
    .filter({ hasText: "qa-product.png" });
  await expect(pickerOption.locator(".asset-option-thumb img")).toBeVisible();
  await pickerOption.locator(".asset-option-thumb").hover();
  const enlarged = page.locator(".asset-option-preview img");
  await expect(enlarged).toBeVisible();
  await expect
    .poll(async () => (await enlarged.boundingBox())?.width ?? 0)
    .toBeGreaterThan(200);
  // It opens beside the list and leaves the file names readable.
  const row = (await pickerOption.boundingBox())!;
  await expect
    .poll(async () => (await enlarged.boundingBox())!.x)
    .toBeGreaterThanOrEqual(row.x + row.width);
  await page.screenshot({
    path: path.resolve("../docs/reference-picker-0.6.21.png"),
  });
  await page.mouse.move(0, 0);
  await expect(enlarged).toBeHidden();
  // Closing the list with the cursor still on a thumbnail closes the preview too.
  await pickerOption.locator(".asset-option-thumb").hover();
  await expect(enlarged).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".asset-option")).toBeHidden();
  await expect(enlarged).toBeHidden();
  await page.mouse.move(0, 0);
  await page.getByRole("button", { name: "开始生成套图", exact: true }).click();
  await expect(page.getByText("等待补充", { exact: true }).first()).toBeVisible(
    { timeout: 30000 },
  );
  expect(imageRequests).toBe(0);
  // The timer stands still while the request waits for the answers.
  const timer = page.getByText(/累计用时 .*计时已暂停/);
  await expect(timer).toBeVisible();
  const paused = await timer.textContent();
  await page.waitForTimeout(2200);
  await expect(timer).toHaveText(paused!);
  // An answer that does not settle it: the question comes back empty, not a failure.
  await page.getByLabel("请补充 P004 真实客户成品机绣规则").fill("上传了");
  await page
    .getByRole("button", { name: "补充并继续制作", exact: true })
    .click();
  let releasePlan = () => {};
  planGate = new Promise((resolve) => (releasePlan = resolve));
  const askedAgain = page.getByLabel(
    "这个回答没有说明规则，请写出 P004 机绣规则",
  );
  await expect(askedAgain).toBeVisible({ timeout: 30000 });
  await expect(askedAgain).toHaveValue("");
  await expect(
    page.getByText("等待补充", { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByText(/Agent 未保存套图方案/)).toHaveCount(0);
  await askedAgain.fill(
    "P004：真实客户机绣，小金毛头像在 MM 中间，袖口黑色脚掌，按参考图比例。",
  );
  cutStreamOnce = true;
  await page
    .getByRole("button", { name: "补充并继续制作", exact: true })
    .click();
  // While it works, the page says which step it is on, not just "处理中".
  const steps = page.getByRole("list", { name: "制作步骤" });
  await expect(steps.locator('[aria-current="step"]')).toContainText(
    "分析资料和参考图",
    { timeout: 30000 },
  );
  await expect(steps).toBeInViewport();
  await expect(steps).toContainText("已查看 1 张参考图");
  await expect(steps).toContainText(/本步已用时 \d+ 秒/);
  await expect(page.getByText(/等待模型回复（已等 \d+ 秒）/)).toBeVisible();
  await expect(
    page.locator(".ant-tag").filter({ hasText: "分析资料和参考图" }),
  ).toBeVisible();
  await expect(page.getByText("处理中", { exact: true })).toHaveCount(0);
  await expect(page.locator("#smart-progress-status")).toContainText(
    "智能生图正在分析：分析资料和参考图",
  );
  // The activity feed shows what Claude Code and the LLM did, not the prompts.
  // It stays folded until opened.
  const feed = page.getByRole("log", { name: "运行详情" });
  const feedToggle = page.locator(".run-activity > summary");
  await expect(feedToggle).toContainText(/运行详情（\d+ 条）/);
  await expect(feed).toBeHidden();
  await feedToggle.click();
  await expect(feed).toBeVisible();
  await expect(feed).toContainText("读取制作需求");
  await expect(feed).toContainText("查看参考图");
  await expect(feed).toContainText(/LLM 请求 #\d+ 完成/);
  await expect(feed).not.toContainText("P004：真实客户机绣");
  await expect(page.locator(".run-activity-facts")).toContainText(
    "次 LLM 请求",
  );
  await page.screenshot({
    path: path.resolve("../docs/run-activity-0.6.24.png"),
  });
  // Coming back, the run details are still there, folded again.
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect(steps).toBeVisible();
  await expect(feedToggle).toBeVisible();
  await expect(feed).toBeHidden();
  releasePlan();
  planGate = null;
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
  // The 2 s and more spent waiting for the answers are left out of the timer.
  const runs = await go<{ items: { waitedMS?: number }[] }>(
    "ListAgentRuns",
    0,
    1,
  );
  expect(runs.items[0].waitedMS || 0).toBeGreaterThanOrEqual(2000);
  // The LLM stream was cut once and retried: a note, not a failure warning.
  expect(cutStreamOnce).toBe(false);
  await expect(
    page.getByText(/LLM 连接中断一次，已自动重试并继续完成/),
  ).toBeVisible();
  await expect(page.getByText(/制作未完成，资料已保留/)).toHaveCount(0);
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
  // The finished set no longer pulls the user back: a new request page, no ring.
  await expect(
    page.getByRole("button", { name: "开始生成套图", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "套图结果" })).toHaveCount(0);
  await expect(page.locator(".nav-progress")).toHaveCount(0);
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
  await expect(dialog.getByText(/右侧女性脱掉米白色针织衫/)).toBeVisible();
  // Let the dialog's opening animation finish so the picture is usable.
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.resolve("../docs/refine-hint-0.6.21.png"),
  });
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
  // The original requirements were first brought in line with the change.
  expect(refineRewrites).toBe(1);
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

test("failed result downloads are retried automatically, then bulk retry never regenerates", async () => {
  returnImageURL = true;
  resultLinkUnavailable = true;
  try {
    for (const slot of [3, 4]) {
      await page
        .getByRole("button", { name: `优化第 ${slot} 张图片`, exact: true })
        .click();
      const dialog = page.getByRole("dialog", { name: "优化这张图片" });
      await dialog.getByLabel("想怎么调整？").fill("稍微提亮背景");
      await dialog
        .getByRole("button", { name: "开始优化", exact: true })
        .click();
      const card = page.locator(".agent-result").nth(slot - 1);
      // First the automatic re-download waits quietly, without a warning.
      if (slot === 3)
        await expect(
          card.getByText(/秒后自动补下载（不会重新生图）/),
        ).toBeVisible({ timeout: 20000 });
      await expect(card.getByText("下载失败", { exact: true })).toBeVisible({
        timeout: 30000,
      });
    }
    const failed = page.locator(".agent-result").nth(2);
    await expect(
      failed.getByText(
        "已收到图片结果，自动补下载后仍未保存。可点“补下载”再试，不会重新生图。",
      ),
    ).toBeVisible();
    await failed.getByText("查看详细错误", { exact: true }).click();
    await expect(
      failed.getByText(/图片链接不可访问或已过期（HTTP 404）/),
    ).toBeVisible();
    await failed.locator("summary").first().focus();
    await page.keyboard.press("Enter");
    await expect(failed.locator(".task-error-details")).not.toHaveAttribute(
      "open",
      "",
    );
    await page.screenshot({
      path: path.resolve("../docs/download-recovery-0.3.11.png"),
      fullPage: true,
    });
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Emulation.setDeviceMetricsOverride", {
      width: 760,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    });
    await expect(
      page.getByRole("button", {
        name: "补下载全部失败图片（2 张）",
        exact: true,
      }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    await cdp.send("Emulation.clearDeviceMetricsOverride");
    await expect(page.locator(".ant-notification-notice")).toHaveCount(0, {
      timeout: 10000,
    });
    const accessibility = await new AxeBuilder({ page }).analyze();
    expect(accessibility.violations).toEqual([]);
    const before = imageRequests;
    resultLinkUnavailable = false;
    await page
      .getByRole("button", { name: "补下载全部失败图片（2 张）", exact: true })
      .click();
    await expect(
      page.locator(".agent-result").filter({ hasText: "下载失败" }),
    ).toHaveCount(0, { timeout: 15000 });
    await expect(
      failed.getByRole("button", { name: "优化第 3 张图片", exact: true }),
    ).toBeVisible();
    expect(imageRequests).toBe(before);
  } finally {
    returnImageURL = false;
    resultLinkUnavailable = false;
  }
});

test("rejected API key is explained and failed images regenerate in one action", async () => {
  rejectImageKey = true;
  const second = page.locator(".agent-result").nth(1);
  await second.getByRole("button", { name: "优化第 2 张图片" }).click();
  const dialog = page.getByRole("dialog", { name: "优化这张图片" });
  await dialog.getByLabel("想怎么调整？").fill("背景更亮一些");
  await dialog.getByRole("button", { name: "开始优化", exact: true }).click();
  await expect(page.getByText("生图中转站拒绝了 API Key")).toBeVisible({
    timeout: 20000,
  });
  await expect(
    second.getByText("API Key 无效，中转站拒绝了请求，未生成也不计费。"),
  ).toBeVisible();
  const bulk = page.getByRole("button", {
    name: "重新生成全部失败图片（1 张）",
  });
  await expect(bulk).toBeVisible();
  rejectImageKey = false;
  const before = imageRequests;
  await bulk.click();
  const confirm = page.getByRole("dialog", { name: "重新生成 1 张图片？" });
  await expect(confirm.getByText("可能产生费用")).toBeVisible();
  await confirm.getByRole("button", { name: "重新生成", exact: true }).click();
  await expect(page.getByText("生图中转站拒绝了 API Key")).toHaveCount(0, {
    timeout: 20000,
  });
  await expect(bulk).toHaveCount(0);
  expect(imageRequests).toBe(before + 1);
  // The ring disappears once the regenerated image is saved and the set is finished.
  await expect(page.locator(".nav-progress")).toHaveCount(0, {
    timeout: 20000,
  });
});

test("an empty relay account is reported as low balance, not as a bad key", async () => {
  emptyImageAccount = true;
  const fourth = page.locator(".agent-result").nth(3);
  await fourth.getByRole("button", { name: "优化第 4 张图片" }).click();
  const dialog = page.getByRole("dialog", { name: "优化这张图片" });
  await dialog.getByLabel("想怎么调整？").fill("颜色更暖一些");
  await dialog.getByRole("button", { name: "开始优化", exact: true }).click();
  await expect(page.getByText("生图中转站账户余额不足")).toBeVisible({
    timeout: 20000,
  });
  await expect(
    fourth.getByText(
      "中转站账户余额不足，中转站拒绝了请求，未生成也不计费。请到中转站后台充值后重新生成。",
    ),
  ).toBeVisible();
  await expect(page.getByText("生图中转站拒绝了 API Key")).toHaveCount(0);
  await expect(page.getByText(/API Key 无效/)).toHaveCount(0);
  emptyImageAccount = false;
  const before = imageRequests;
  await page
    .getByRole("button", { name: "重新生成全部失败图片（1 张）" })
    .click();
  await page
    .getByRole("dialog", { name: "重新生成 1 张图片？" })
    .getByRole("button", { name: "重新生成", exact: true })
    .click();
  await expect(page.getByText("生图中转站账户余额不足")).toHaveCount(0, {
    timeout: 20000,
  });
  expect(imageRequests).toBe(before + 1);
  await expect(page.locator(".nav-progress")).toHaveCount(0, {
    timeout: 20000,
  });
});

test("an image the content check refuses is resent, then reworded by the LLM, without a warning", async () => {
  refuseImages = 2;
  const before = imageRequests;
  const fifth = page.locator(".agent-result").nth(4);
  await fifth.getByRole("button", { name: "优化第 5 张图片" }).click();
  const dialog = page.getByRole("dialog", { name: "优化这张图片" });
  await dialog.getByLabel("想怎么调整？").fill("背景换成圣诞客厅");
  await dialog.getByRole("button", { name: "开始优化", exact: true }).click();
  await expect(fifth.getByText("内容审核拒绝后已改写措辞自动重发")).toBeVisible(
    { timeout: 30000 },
  );
  await expect(
    fifth.getByRole("button", { name: "优化第 5 张图片" }),
  ).toBeVisible({ timeout: 20000 });
  expect(refusedRequests).toBe(2);
  expect(rewriteRequests).toBe(1);
  expect(imageRequests).toBe(before + 1);
  // Nothing to act on: no warning, no bulk retry.
  await expect(fifth.locator(".ant-alert")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: /重新生成全部失败图片/ }),
  ).toHaveCount(0);
  const tasks = await go<{ items: any[] }>("ListTasks", "", 0, 5);
  const [done, refused] = tasks.items;
  expect(done.policyStep).toBe(2);
  expect(done.prompt).toContain("改写后：");
  expect(refused.policyRetriedAs).toBe(done.id);
  expect(refused.error).toContain("生图模型的内容审核拒绝了这张图");
  await expect(page.locator(".nav-progress")).toHaveCount(0, {
    timeout: 20000,
  });
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
  await expect(page.locator("#smart-progress-status")).toContainText(
    "智能生图正在分析",
  );
  await expect(page.getByRole("list", { name: "制作步骤" })).toContainText(
    "网站拦截，改用浏览器读取",
  );
  await page.getByRole("link", { name: "素材库", exact: true }).click();
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
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
  // An unfinished run (waiting for input) is kept when the user leaves and returns.
  await expect(page.locator("#smart-progress-status")).toContainText(
    "智能生图等待补充资料",
  );
  await page.getByRole("link", { name: "任务中心", exact: true }).click();
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
  await expect(
    page.getByText("等待补充", { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByLabel("商品链接", { exact: true })).toHaveValue(
    shopURL,
  );
  const runs = await go<{ items: any[] }>("ListAgentRuns", 0, 50);
  const run = runs.items.find((r) => r.input.productURL === shopURL);
  expect(run.product.title).toBe(shopTitle);
  expect(run.product.error).toBe("");
  expect(run.product.imageNote).toBe("部分商品图片不可读取，可上传参考图");
  expect(run.product.referenceIDs).toHaveLength(1);
  // The page was read, so the "cannot read the link" warning stays hidden and a
  // quiet note mentions the skipped image instead.
  await expect(page.getByText(/暂时无法读取商品链接/)).toHaveCount(0);
  await expect(
    page.getByText("已读取商品页，部分商品图片不可读取，可上传参考图。"),
  ).toBeVisible();
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

test("automatic mode never asks and delivers the whole set", async () => {
  test.setTimeout(90000);
  const before = imageRequests;
  await page.getByRole("link", { name: "智能生图", exact: true }).click();
  await page.getByRole("button", { name: "新建需求", exact: true }).click();
  await page.getByLabel("商品链接", { exact: true }).fill("");
  await page
    .getByLabel("完整制作指令", { exact: true })
    .fill("全自动 Etsy 套图：小金毛刺绣卫衣，12张，1:1。");
  expect(await go<string>("SmartMode")).toBe("manual");
  const mode = page
    .locator(".page-header")
    .getByRole("group", { name: "制作模式" });
  await expect(mode).toBeVisible();
  const manual = mode.getByRole("radio", { name: /^手动/ });
  const auto = mode.getByRole("radio", { name: /^自动/ });
  await expect(manual).toBeChecked();
  // Each option says what it does without reading the subtitle.
  await expect(mode).toContainText("缺资料时停下来问你");
  await expect(mode).toContainText("助手代答，标出需核对");
  // Keyboard: arrow keys move between the two modes.
  await manual.focus();
  await page.keyboard.press("ArrowRight");
  await expect(auto).toBeChecked();
  await expect.poll(() => go<string>("SmartMode")).toBe("auto");
  await page.keyboard.press("ArrowLeft");
  await expect(manual).toBeChecked();
  await expect.poll(() => go<string>("SmartMode")).toBe("manual");
  await auto.click();
  await expect(auto).toBeChecked();
  await expect.poll(() => go<string>("SmartMode")).toBe("auto");
  const modeA11y = await new AxeBuilder({ page })
    .include(".page-header")
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(modeA11y.violations).toEqual([]);
  await page.locator(".page-header").screenshot({
    path: path.resolve("../docs/smart-mode-switch-0.7.7.png"),
  });
  await expect(
    page.getByText(/自动模式：给完指令直接得到一套图/),
  ).toBeVisible();
  await expect(page.getByLabel("完整制作指令", { exact: true })).toHaveValue(
    /全自动 Etsy/,
  );
  await page.getByRole("button", { name: "开始生成套图", exact: true }).click();
  await expect(page.getByText("自动模式", { exact: true })).toBeVisible();
  await expect(
    page.getByText("已完成 12 / 12 个任务，可下载 12 张图片", { exact: true }),
  ).toBeVisible({ timeout: 60000 });
  await expect(page.getByRole("heading", { name: "补充必要资料" })).toHaveCount(
    0,
  );
  // It shows how it answered each question; the picked name is flagged.
  const judgments = page.getByRole("region", { name: /制作助手的判断/ });
  await expect(judgments.getByRole("listitem")).toHaveCount(4);
  await expect(judgments).toContainText("其中 1 项需核对");
  for (const label of ["指令", "需核对", "自行选择", "原链接"])
    await expect(judgments.getByText(label, { exact: true })).toBeVisible();
  await expect(judgments).toContainText("原链接里的红绿迷你圣诞袜帽");
  expect(autoOfferedAnswers).toBe(true);
  // The first sample failed its check and was redrawn once before the set.
  expect(imageRequests).toBe(before + 13);
  expect(sampleChecks).toBe(3);
  // Every image names the chest master and the cuff sample it was drawn from.
  await expect(
    page
      .getByText(
        "已照第 12 张「胸前」样板图、第 11 张「袖口」样板图画对应的设计。",
      )
      .first(),
  ).toBeVisible();
  await expect(
    page.getByText("已按核对意见重画 1 次，样板图已核对，与设计依据一致。"),
  ).toBeVisible();
  const runs = await go<{ items: any[] }>("ListAgentRuns", 0, 1);
  expect(runs.items[0].input.autoMode).toBe(true);
  expect(
    runs.items[0].questions.map((q: any) => q.answeredBy + ":" + q.basis),
  ).toEqual([
    "agent:instruction",
    "agent:verify",
    "agent:choice",
    "agent:source",
  ]);
  await page.screenshot({
    path: path.resolve("../docs/smart-auto-mode-0.7.7.png"),
  });
  // Narrow windows: the switch wraps under the title without side scrolling.
  const cdp = await browser.contexts()[0].newCDPSession(page);
  for (const width of [900, 700]) {
    await cdp.send("Emulation.setDeviceMetricsOverride", {
      width,
      height: 740,
      deviceScaleFactor: 1,
      mobile: false,
    });
    await expect(auto).toBeChecked();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      ),
    ).toBe(false);
    await page.locator(".page-header").screenshot({
      path: path.resolve(`../docs/smart-mode-switch-${width}-0.7.7.png`),
    });
  }
  await cdp.send("Emulation.clearDeviceMetricsOverride");
  await cdp.detach();
  // Back to manual, the default every other test expects.
  await manual.click();
  await expect(manual).toBeChecked();
  await expect.poll(() => go<string>("SmartMode")).toBe("manual");
  await expect(page.getByText(/手动模式：输入制作需求/)).toBeVisible();
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
test("LLM monitor shows requests, tokens, the retried drop and capability tests", async () => {
  await page.getByRole("link", { name: "LLM 监控", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "LLM 监控", level: 1 }),
  ).toBeVisible();
  // The twelve-image run had one cut stream that Claude Code retried.
  const drops = page.locator(".drop-item");
  await expect(drops.first()).toBeVisible();
  await expect(
    page.locator(".drop-item").filter({ hasText: "已自动重试" }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("cell", { name: "已提交 12 张" }).first(),
  ).toBeVisible();
  const card = page
    .locator(".monitor-card")
    .filter({ hasText: "Agent QA LLM" });
  await expect(card).toContainText("能力验证通过");
  expect(
    Number(await card.locator(".monitor-big strong").innerText()),
  ).toBeGreaterThan(4);
  await expect(card).not.toContainText("输入 0 tokens");
  await page
    .getByRole("combobox", { name: "请求类型" })
    .locator("..")
    .locator("..")
    .click();
  await page
    .locator(
      '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option[title="能力验证"]',
    )
    .click();
  const rows = page.locator(
    'section[aria-labelledby="llm-log"] .ant-table-tbody tr.ant-table-row',
  );
  await expect(rows.first()).toContainText("能力验证");
  await expect(page.getByText("这段时间没有智能生图分析")).toBeVisible();
  await page
    .getByRole("combobox", { name: "请求类型" })
    .locator("..")
    .locator("..")
    .click();
  await page
    .locator(
      '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option[title="全部"]',
    )
    .click();
  await page
    .getByRole("combobox", { name: "请求结果" })
    .locator("..")
    .locator("..")
    .click();
  await page
    .locator(
      '.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option[title="连接中断"]',
    )
    .click();
  await expect(rows.first()).toContainText("连接中断");
  await expect(
    page.locator(".ant-select-dropdown:not(.ant-select-dropdown-hidden)"),
  ).toHaveCount(0);
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
  await page.screenshot({
    path: "../docs/llm-monitor-0.5.4.png",
    fullPage: true,
  });
  await cdp.send("Emulation.clearDeviceMetricsOverride");
  await cdp.detach();
});
