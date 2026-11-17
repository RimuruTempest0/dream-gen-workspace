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
import { inflateSync } from "node:zlib";

// The admin tool in an isolated QA instance: a stand-in source folder and a fake
// builder, so packages are real ZIPs of small test files. The mock LLM answers
// the app's capability check (text, image, tool call, streaming).
let browser: Browser, page: Page, server: http.Server, base: string;
const repo = fs
  .readFileSync(
    path.resolve(import.meta.dirname, "../../.tools/admin-qa-repo.txt"),
    "utf8",
  )
  .trim();

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

// Scan only once dialogs, dropdowns and notifications have finished animating.
async function settle() {
  await expect(page.locator(".ant-modal-mask")).toHaveCount(0);
  await expect(
    page.locator(".ant-select-dropdown:not(.ant-select-dropdown-hidden)"),
  ).toHaveCount(0);
  await expect(page.locator(".ant-notification-notice")).toHaveCount(0, {
    timeout: 15000,
  });
  // Buttons fade between disabled and enabled; a scan mid-fade reads a colour
  // that is on screen for a few frames only.
  await page.waitForFunction(() => document.getAnimations().length === 0);
}

test.describe.configure({ mode: "serial" });
test.beforeAll(async () => {
  server = http.createServer((req, res) => {
    let raw = "";
    req.on("data", (c) => (raw += c));
    req.on("end", () => {
      if (req.url?.endsWith("/models")) {
        res.setHeader("content-type", "application/json");
        res.end(JSON.stringify({ data: [{ id: "mock-agent" }] }));
        return;
      }
      const body = JSON.parse(raw || "{}");
      const first = body.messages?.[0]?.content;
      if (body.stream) {
        res.setHeader("content-type", "text/event-stream");
        const frame = (v: unknown) =>
          res.write(
            `event: ${(v as { type: string }).type}\ndata: ${JSON.stringify(v)}\n\n`,
          );
        frame({
          type: "message_start",
          message: {
            id: "m",
            type: "message",
            role: "assistant",
            model: "mock-agent",
            content: [],
            usage: { input_tokens: 1, output_tokens: 0 },
          },
        });
        frame({
          type: "content_block_start",
          index: 0,
          content_block: { type: "text", text: "" },
        });
        frame({
          type: "content_block_delta",
          index: 0,
          delta: { type: "text_delta", text: "OK" },
        });
        frame({ type: "content_block_stop", index: 0 });
        frame({
          type: "message_delta",
          delta: { stop_reason: "end_turn" },
          usage: { output_tokens: 1 },
        });
        frame({ type: "message_stop" });
        res.end();
        return;
      }
      let content: unknown[] = [{ type: "text", text: "OK" }];
      if (Array.isArray(first) && first[0]?.type === "image")
        content = [{ type: "text", text: imageColor(first[0].source.data) }];
      else if (
        typeof first === "string" &&
        first.startsWith("Call capability_echo with nonce ")
      )
        content = [
          {
            type: "tool_use",
            id: "t1",
            name: "capability_echo",
            input: {
              nonce: first.replace("Call capability_echo with nonce ", ""),
            },
          },
        ];
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ content }));
    });
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", () => r()));
  base = `http://127.0.0.1:${(server.address() as { port: number }).port}/v1`;
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
});
test.afterAll(async () => {
  await browser?.close();
  await new Promise<void>((r) => server.close(() => r()));
});

async function addUpstream(
  name: string,
  kind: "图片中转站" | "LLM 供应商",
  model: string,
  perKeyQuota = false,
) {
  await page.getByRole("button", { name: "添加上游" }).click();
  const dialog = page.getByRole("dialog", { name: "添加上游" });
  await dialog.getByLabel("名称").fill(name);
  if (kind === "LLM 供应商") {
    await dialog.getByLabel("类型").locator("..").locator("..").click();
    await page
      .locator(
        `.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option[title="${kind}"]`,
      )
      .click();
  }
  await dialog.getByLabel("API 地址").fill(base);
  await dialog.getByLabel("模型").fill(model);
  if (perKeyQuota)
    await dialog
      .getByRole("checkbox", { name: /可以给单个 Key 设硬额度/ })
      .check();
  await dialog.getByRole("button", { name: "保存" }).click();
  await expect(dialog).toBeHidden();
}

async function importKeys(
  upstream: string,
  use: "主用" | "备用",
  keys: string[],
) {
  const form = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "导入 Key" }) });
  await form.getByLabel("上游").locator("..").locator("..").click();
  await page
    .locator(
      `.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option[title^="${upstream}"]`,
    )
    .click();
  await form.getByLabel("用途").locator("..").locator("..").click();
  await page
    .locator(
      `.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option[title="${use}"]`,
    )
    .click();
  await form.getByLabel("Key（每行一个）").fill(keys.join("\n"));
  await form.getByRole("button", { name: "导入" }).click();
  await expect(page.getByText(`已导入 ${keys.length} 个`).last()).toBeVisible();
}

test("管理工具：上游与 Key 池、客户、自动分配、能力验证、交付包与换 Key", async () => {
  // The tool has its own version, separate from the app's.
  await expect(page.locator(".sidebar-bottom")).toContainText(
    /管理工具 \d+\.\d+\.\d+/,
  );
  await page.getByRole("link", { name: "设置与签名密钥" }).click();
  await expect(page.getByText("公钥指纹")).toBeVisible();
  await expect(page.getByText(repo)).toBeVisible();

  await page.getByRole("link", { name: "Key 池" }).click();
  // Sub2API-like image relay: per-key quota; DeepSeek-like LLM: none.
  await addUpstream("QA 幻灵", "图片中转站", "gpt-image-2", true);
  await addUpstream("QA LLM", "LLM 供应商", "mock-agent");
  const k = (p: string, n: number) =>
    Array.from({ length: n }, (_, i) => `sk-qa-${p}-${i}-0123456789`);
  await importKeys("QA 幻灵", "主用", k("prim", 4));
  await importKeys("QA 幻灵", "备用", k("back", 3));
  await expect(page.getByText("这个上游不能限制单个 Key")).toHaveCount(0);
  await importKeys("QA LLM", "主用", k("llm-prim", 1));
  await expect(page.getByText("这个上游不能限制单个 Key")).toBeVisible();
  await importKeys("QA LLM", "备用", k("llm-back", 1));
  await expect(page.getByText("无单 Key 额度")).toHaveCount(2);
  // The same key again is skipped, never added twice.
  const form = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "导入 Key" }) });
  const keyCount = async () =>
    page.evaluate(
      async () =>
        ((await window.go!.main!.App!.State()) as { keys: unknown[] }).keys
          .length,
    );
  const before = await keyCount();
  await form.getByLabel("Key（每行一个）").fill(k("prim", 1)[0]);
  await form.getByRole("button", { name: "导入" }).click();
  await expect(form.getByLabel("Key（每行一个）")).toHaveValue("");
  expect(await keyCount()).toBe(before);
  await expect(page.getByText("sk-qa-")).toHaveCount(0);

  // Verify the three LLM keys (mock LLM; the confirm warns about the cost).
  for (let i = 0; i < 2; i++) {
    await page.getByRole("button", { name: "验证能力" }).first().click();
    const confirm = page.getByRole("dialog", {
      name: "验证这个 LLM Key 的能力？",
    });
    await expect(
      confirm.getByText(/会用这个 Key 发 4 个很短的 LLM 请求/),
    ).toBeVisible();
    await confirm.getByRole("button", { name: "验证（产生少量费用）" }).click();
    await expect(page.getByText("能力验证通过").last()).toBeVisible({
      timeout: 30000,
    });
  }
  await expect(page.getByRole("button", { name: "验证能力" })).toHaveCount(0);

  await page.getByRole("link", { name: "客户" }).click();
  await page.getByRole("button", { name: "新建", exact: true }).click();
  const create = page.getByRole("dialog", { name: "新建客户或顾客" });
  // The e-commerce line is the default.
  await expect(create.getByRole("radio", { name: "电商版" })).toBeChecked();
  await create.getByLabel("名称").fill("QA 客户");
  await create.getByRole("button", { name: "创建" }).click();
  await expect(
    page.getByRole("heading", { name: "QA 客户", level: 1 }),
  ).toBeVisible();
  // Only image line 1 and LLM line 1 are required for a package.
  await expect(page.getByText("图片 · 主线路 · 必需")).toHaveCount(1);
  await expect(page.getByText("图片 · 备用 · 至少一条")).toHaveCount(5);
  await expect(page.getByText("LLM · 备用 · 可选")).toHaveCount(1);
  await expect(page.getByRole("button", { name: "生成交付包" })).toBeDisabled();
  await page.getByRole("button", { name: "自动分配空缺线路" }).click();
  await expect(
    page.getByText("LLM 线路 1、LLM 线路 2不能单独限额"),
  ).toBeVisible();
  await expect(
    page.locator(".ant-alert-message").filter({ hasText: "图片线路" }),
  ).toHaveCount(0);
  // Payment: the quotas were raised upstream, then the code may be sent.
  await expect(page.getByText("试用中", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "已收款并调高额度" }).click();
  await page
    .getByRole("dialog", { name: "确认已收款并调高额度？" })
    .getByRole("button", { name: "已调高，标记收款" })
    .click();
  await expect(page.getByText("已收款 · 可发激活码")).toBeVisible();
  await expect(page.getByText("待能力验证")).toHaveCount(0);
  await expect(
    page.locator(".ant-tag").filter({ hasText: "未分配" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "显示" }).click();
  await expect(page.locator(".admin-code code")).toHaveText(
    /^[0-9A-Z]{5}(-[0-9A-Z]{5}){4}$/,
  );
  const code = await page.locator(".admin-code code").innerText();

  await page.getByRole("button", { name: "图片线路 3 上移" }).click();
  await expect(page.locator(".admin-order li").first()).toContainText(
    "图片线路 3",
  );

  // Built-in skills: all ticked by default; unticking one is saved per customer.
  const skillsPanel = page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "内置技能" }) });
  await expect(skillsPanel.getByRole("switch")).toHaveCount(5);
  for (const box of await skillsPanel.getByRole("switch").all())
    await expect(box).toBeChecked();
  const p004 = skillsPanel.getByRole("switch", {
    name: "交付包带上 P004 生图规则",
  });
  await p004.click();
  await expect(p004).not.toBeChecked();
  await expect
    .poll(async () => {
      const st = await page.evaluate(() => (window as any).go.main.App.State());
      return st.customers.find((x: { name: string }) => x.name === "QA 客户")
        .skills;
    })
    .toHaveLength(4);
  await p004.click();
  await expect(p004).toBeChecked();
  await expect
    .poll(async () => {
      const st = await page.evaluate(() => (window as any).go.main.App.State());
      return st.customers.find((x: { name: string }) => x.name === "QA 客户")
        .skills;
    })
    .toBeUndefined();

  // Expiry: a real field, signed into the package; renewal needs a package first.
  await expect(
    page.getByRole("button", { name: "生成续费授权文件" }),
  ).toBeDisabled();
  await page.getByLabel(/有效期至/).fill("2030-06-30");
  await page
    .locator("section")
    .filter({ has: page.getByRole("heading", { name: "套餐与额度" }) })
    .getByRole("button", { name: "保存" })
    .click();
  await expect(page.getByText("当前有效期至 2030")).toBeVisible();
  const saved = await page.evaluate(() => (window as any).go.main.App.State());
  expect(
    saved.customers.find((x: { name: string }) => x.name === "QA 客户")
      .expiresAt,
  ).toMatch(/^2030-06-30T|^2030-06-29T/);

  await page.getByRole("button", { name: "生成交付包" }).click();
  await expect(page.getByText("交付包已生成。")).toBeVisible({
    timeout: 60000,
  });
  await expect(
    page.getByRole("button", { name: "生成续费授权文件" }),
  ).toBeEnabled();
  const softwareID = (await page.locator(".page-header p").innerText()).match(
    /IW-[0-9A-Z-]+/,
  )![0];
  const zip = path.join(
    repo,
    "release",
    "customers",
    softwareID,
    "ImageWorkbench-QA 客户-0.6.2.zip",
  );
  expect(fs.existsSync(zip)).toBe(true);
  // The package is the e-commerce line: built in its own build folder from main
  // (0.6.2), while the source folder stays on the mass-market branch (0.7.34).
  await expect(page.getByText(/构建电商版：main 分支的提交/)).toBeVisible();
  expect(
    fs.readFileSync(
      path.join(repo + "-build", "ecommerce", "wails.json"),
      "utf8",
    ),
  ).toContain("0.6.2");
  expect(fs.readFileSync(path.join(repo, "wails.json"), "utf8")).toContain(
    "0.7.34",
  );
  const dat = fs.readFileSync(
    path.join(
      repo,
      "release",
      "customers",
      softwareID,
      "0.6.2",
      "customer.dat",
    ),
    "utf8",
  );
  expect(dat).not.toContain("sk-qa-");
  expect(dat).not.toContain(code);
  await expect(
    page.locator(".ant-table-row").filter({ hasText: "0.6.2" }),
  ).toBeVisible();

  // Replace a key after delivery: the old one is retired and a new package is due.
  await page
    .getByRole("combobox", { name: "图片线路 1 的 Key" })
    .locator("..")
    .locator("..")
    .click();
  await page
    .locator(
      ".ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option",
    )
    .last()
    .click();
  await page
    .getByRole("dialog", { name: "更换图片线路 1的 Key？" })
    .getByRole("button", { name: "更换" })
    .click();
  await expect(page.getByText(/线路有变更/).first()).toBeVisible();

  await settle();
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
    path: "../docs/admin-tool-0.6.2.png",
    fullPage: true,
  });
  await cdp.send("Emulation.clearDeviceMetricsOverride");
  await cdp.detach();

  await page.getByRole("link", { name: "Key 池" }).click();
  const retired = page.locator(".ant-table-row").filter({ hasText: "已停用" });
  await expect(retired).toHaveCount(1);
  await settle();
  const a11yKeys = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11yKeys.violations).toEqual([]);

  // Deleting the customer: its delivered keys stay retired, none stays assigned.
  await page.getByRole("link", { name: "客户", exact: true }).click();
  await page.getByRole("link", { name: "QA 客户" }).click();
  await page.getByRole("button", { name: "删除客户" }).click();
  await page
    .getByRole("dialog", { name: "删除客户“QA 客户”？" })
    .getByRole("button", { name: "删除客户" })
    .click();
  await expect(page.getByText("还没有客户")).toBeVisible();
  await page.getByRole("link", { name: "Key 池" }).click();
  await expect(
    page.locator(".ant-table-row").filter({ hasText: "已分配" }),
  ).toHaveCount(0);
  const disabled = page.locator(".ant-table-row").filter({ hasText: "已停用" });
  await expect(disabled.first()).toBeVisible();

  // Re-enabling a delivered key warns that it is in a delivered package.
  const disabledCount = await disabled.count();
  await disabled.first().getByRole("button", { name: "重新启用" }).click();
  const confirm = page.getByRole("dialog", { name: "重新启用这个 Key？" });
  await expect(confirm).toContainText("已随交付包发给客户");
  await confirm.getByRole("button", { name: "重新启用" }).click();
  await expect(disabled).toHaveCount(disabledCount - 1);
  await expect(
    page.locator(".ant-table-row").filter({ hasText: "曾交付" }),
  ).toHaveCount(1);

  // Bulk: disable every free key; a once-delivered key in the selection blocks delete.
  const pickStatus = async (label: string) => {
    await page
      .locator(".ant-select")
      .filter({ has: page.getByRole("combobox", { name: "按状态筛选" }) })
      .click();
    await page.locator(`.ant-select-item-option[title="${label}"]`).click();
  };
  await pickStatus("可用");
  // Only the key table: the upstream table above also has rows.
  const keyRows = page
    .getByRole("region", { name: "Key 列表，可左右滚动" })
    .locator(".ant-table-row");
  await expect(keyRows.filter({ hasNotText: "可用" })).toHaveCount(0);
  const freeCount = await keyRows.count();
  expect(freeCount).toBeGreaterThan(1);
  await page.getByRole("checkbox", { name: "选择本页 Key" }).check();
  const bar = page.getByRole("group", { name: "批量操作" });
  await expect(bar).toContainText(`已选 ${freeCount} 个 Key`);
  await expect(bar).toContainText("1 个交付过，只能停用、不能删除");
  await expect(bar.getByRole("button", { name: "删除所选" })).toBeDisabled();
  await settle();
  const a11yBulk = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11yBulk.violations).toEqual([]);
  await bar.getByRole("button", { name: "停用所选" }).click();
  await page
    .getByRole("dialog", { name: `停用 ${freeCount} 个 Key？` })
    .getByRole("button", { name: "停用" })
    .click();
  await expect(page.getByText("没有符合条件的 Key")).toBeVisible();

  // Bulk delete the never-delivered ones; the once-delivered key stays.
  await pickStatus("已停用");
  const spare = keyRows
    .filter({ hasText: "手动停用" })
    .filter({ hasNotText: "曾交付" });
  const spareCount = await spare.count();
  expect(spareCount).toBe(freeCount - 1);
  for (let i = 0; i < spareCount; i++)
    await spare.nth(i).getByRole("checkbox").check();
  await bar.getByRole("button", { name: "删除所选" }).click();
  await page
    .getByRole("dialog", { name: `删除 ${spareCount} 个 Key？` })
    .getByRole("button", { name: "删除" })
    .click();
  await expect(spare).toHaveCount(0);
  await expect(
    page.locator(".ant-table-row").filter({ hasText: "曾交付" }),
  ).toHaveCount(1);
});

test("发布大众版：显示将要构建的提交，未提交的改动不进包，源码目录不被改动", async () => {
  const { execFileSync } = await import("node:child_process");
  const git = (...args: string[]) =>
    execFileSync("git", ["-C", repo, ...args], { stdio: "pipe" })
      .toString()
      .trim();
  const build = page.getByRole("button", { name: /^构建大众版/ });
  const edited = '{ "info": { "productVersion": "0.7.34" } }\n// edited';
  try {
    // The page shows the newest commit of the line's branch; no branch to switch.
    await page.getByRole("link", { name: "发布大众版" }).click();
    await expect(page.getByText("QA creative source")).toBeVisible({
      timeout: 15000,
    });
    await expect(
      page.getByText("creative-concepts", { exact: true }),
    ).toBeVisible();
    await expect(build).toBeEnabled();
    await expect(build).toHaveText(/0\.7\.34/);

    // Uncommitted work in the source folder is pointed out and does not block.
    fs.writeFileSync(path.join(repo, "wails.json"), edited);
    await expect(
      page.getByText("源码目录有 1 个未提交的改动，不会进入这次构建。"),
    ).toBeVisible({ timeout: 15000 });
    await expect(build).toBeEnabled();

    // The notes come from the changelog of that commit, without its verification part.
    const notes = page.getByRole("textbox", { name: /更新说明/ });
    await expect(notes).toHaveValue(/QA creative notes, first paragraph\./);
    await expect(notes).not.toHaveValue(/not for people using the app/);
    await build.click();
    await expect(page.getByText("大众版已构建并签名。")).toBeVisible({
      timeout: 60000,
    });
    await expect(page.getByText("0.7.34 已准备好，还没有上传")).toBeVisible();

    // Built from the commit, in the line's build folder; the source folder keeps
    // its branch and its uncommitted work.
    expect(
      fs.readFileSync(
        path.join(repo + "-build", "creative", "wails.json"),
        "utf8",
      ),
    ).not.toContain("edited");
    expect(git("rev-parse", "--abbrev-ref", "HEAD")).toBe("creative-concepts");
    expect(fs.readFileSync(path.join(repo, "wails.json"), "utf8")).toBe(edited);

    const out = path.join(repo, "release", "creative", "0.7.34");
    expect(fs.readdirSync(path.join(out, "upload")).sort()).toEqual([
      "ImageWorkbench-update-0.7.34.zip",
      "update.json",
    ]);
    expect(fs.existsSync(path.join(out, "ImageWorkbench-0.7.34.zip"))).toBe(
      true,
    );
    // Format 2 with the line: delivered e-commerce apps (format 1) refuse it.
    const manifest = JSON.parse(
      fs.readFileSync(path.join(out, "upload", "update.json"), "utf8"),
    );
    expect(manifest.format).toBe(2);
    const payload = JSON.parse(
      Buffer.from(manifest.payload, "base64").toString("utf8"),
    );
    expect(payload).toMatchObject({
      format: 2,
      line: "creative",
      version: "0.7.34",
      url: "https://github.com/RimuruTempest0/dream-gen-workspace/releases/download/creative/ImageWorkbench-update-0.7.34.zip",
    });
    await expect(
      page.locator(".ant-table-row").filter({ hasText: "0.7.34" }),
    ).toBeVisible();

    await settle();
    const a11y = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
      .analyze();
    expect(a11y.violations).toEqual([]);
    await page.screenshot({
      path: "../docs/admin-creative-1.7.0.png",
      fullPage: true,
    });
  } finally {
    git("checkout", "-q", "--", "wails.json");
  }
});

test("大众版托管版：新建顾客，用已构建的大众版程序加授权文件出包，不重新构建", async () => {
  // More keys for a second person (the first customer took the pool), through
  // the same calls the Key 池 page makes; the LLM keys are verified on the mock.
  type Key = { id: string; upstreamID: string; verified: boolean };
  type St = {
    upstreams: { id: string; name: string }[];
    keys: Key[];
    customers: { name: string; softwareID: string; line?: string }[];
  };
  const app = <T>(name: string, ...args: unknown[]) =>
    page.evaluate(
      ([n, a]) => (window as any).go.main.App[n as string](...(a as [])),
      [name, args] as const,
    ) as Promise<T>;
  const st = await app<St>("State");
  const relay = st.upstreams.find((u) => u.name === "QA 幻灵")!.id;
  const llm = st.upstreams.find((u) => u.name === "QA LLM")!.id;
  await app("ImportKeys", relay, "primary", "sk-qa-hosted-prim-0123456789", "");
  await app("ImportKeys", relay, "backup", "sk-qa-hosted-back-0123456789", "");
  await app("ImportKeys", llm, "primary", "sk-qa-hosted-llm-0123456789", "");
  for (const k of (await app<St>("State")).keys)
    if (k.upstreamID === llm && !k.verified) await app("VerifyLLM", k.id);

  await page.getByRole("link", { name: "客户" }).click();
  await page.getByRole("button", { name: "新建", exact: true }).click();
  const create = page.getByRole("dialog", { name: "新建客户或顾客" });
  await create.getByRole("radio", { name: "大众版 · 托管版" }).check();
  await create.getByLabel("名称").fill("QA 顾客");
  await create.getByRole("button", { name: "创建" }).click();
  await expect(
    page.getByRole("heading", { name: "QA 顾客", level: 1 }),
  ).toBeVisible();
  await expect(page.locator(".page-header p")).toContainText("大众版 · 托管版");
  // The package is made of the version built on the 发布大众版 page.
  const build = page.getByRole("button", { name: /^生成交付包/ });
  await expect(build).toBeDisabled();
  await expect(build).toHaveText(/0\.7\.34/);
  // A hosted user who already has the program needs the licence file only.
  await page.getByRole("button", { name: "自动分配空缺线路" }).click();
  // Fewer keys than lines: the optional lines stay empty, which is said once.
  const partial = page.getByRole("dialog", {
    name: "部分可选线路没有可用的 Key",
  });
  await partial.getByRole("button").last().click();
  await expect(page.locator(".ant-modal-wrap:visible")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "只生成授权文件" }),
  ).toBeEnabled();
  await expect(build).toBeEnabled();
  await build.click();
  await expect(page.getByText("交付包已生成。")).toBeVisible({
    timeout: 60000,
  });
  await expect(page.getByText(/不重新构建/).first()).toBeVisible();
  const person = (await app<St>("State")).customers.find(
    (c) => c.name === "QA 顾客",
  )!;
  expect(person.line).toBe("creative");
  const out = path.join(repo, "release", "customers", person.softwareID);
  const zip = path.join(out, "ImageWorkbench-QA 顾客-0.7.34.zip");
  expect(fs.existsSync(zip)).toBe(true);
  // Put together, not built: no program folder for this person, and the
  // licence file is in the package (stored names are plain in a ZIP).
  expect(fs.existsSync(path.join(out, "0.7.34"))).toBe(false);
  const bytes = fs.readFileSync(zip);
  expect(bytes.includes(Buffer.from("customer.dat"))).toBe(true);
  expect(bytes.includes(Buffer.from("sk-qa-hosted"))).toBe(false);
  expect(
    fs.existsSync(
      path.join(repo, "release", "creative", "build", "0.7.34", "customer.dat"),
    ),
  ).toBe(false);

  // The list tells the two product lines apart (the first test deleted its
  // customer, so an e-commerce one is created here).
  await app("CreateCustomer", "QA 客户 乙", 0, "ecommerce");
  await page.getByRole("link", { name: "客户", exact: true }).click();
  const row = (name: string) =>
    page.locator(".ant-table-row").filter({ hasText: name });
  await expect(row("QA 顾客")).toContainText("大众版 · 托管版");
  await expect(row("QA 客户 乙")).toContainText("电商版");
  await settle();
  const a11y = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);
  await page.screenshot({
    path: "../docs/admin-hosted-1.8.0.png",
    fullPage: true,
  });
});
