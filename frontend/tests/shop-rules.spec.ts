import {
  test,
  expect,
  chromium,
  type Browser,
  type Page,
} from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import path from "node:path";

let browser: Browser, page: Page;
async function go<T>(name: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(
    ([n, a]) => (window.go!.main!.App as any)[n as string](...(a as [])),
    [name, args] as const,
  ) as Promise<T>;
}

test.beforeAll(async () => {
  browser = await chromium.connectOverCDP(
    process.env.DESKTOP_CDP_URL || "http://127.0.0.1:9223",
  );
  page = browser.contexts().flatMap((c) => c.pages())[0];
  await page.waitForFunction(() => !!window.go?.main?.App);
  await go("ResetDesktopQA");
});
test.afterAll(async () => {
  await page?.goto("http://wails.localhost/#/");
  await browser?.close();
});

// 创作's entry keeps the link, references and settings under 更多选项.
async function openMore() {
  const more = page.getByRole("button", { name: "更多选项", exact: true });
  if (
    (await more.count()) &&
    (await more.getAttribute("aria-expanded")) === "false"
  )
    await more.click();
}

test("店铺规则：新建、附件、启停；技能按品类归类，创作可选品类", async () => {
  await page.goto("http://wails.localhost/#/settings");
  const panel = page.locator("#shop-rules");
  await expect(panel.getByText("还没有店铺规则")).toBeVisible();
  await panel.getByRole("button", { name: "新建店铺规则" }).click();
  const dialog = page.getByRole("dialog", { name: "新建店铺规则" });
  await dialog.getByLabel("规则名称").fill("QA 店铺");
  await dialog.getByLabel("规则内容").fill("所有图片使用欧美模特。");
  await dialog.getByRole("button", { name: "保存" }).click();
  const row = panel.locator(".connection-row").filter({ hasText: "QA 店铺" });
  await expect(row).toContainText("所有图片使用欧美模特。");
  const rules = await go<{ id: string; enabled: boolean }[]>("ListShopRules");
  expect(rules[0].enabled).toBe(true);
  await go("AddShopRuleFilesFromPaths", rules[0].id, [
    path.resolve(import.meta.dirname, "fixture.png"),
  ]);
  await page.reload();
  await page.waitForFunction(() => !!window.go?.main?.App);
  await expect(row.getByText("fixture.png")).toBeVisible();
  await row.getByRole("switch", { name: "启用店铺规则 QA 店铺" }).click();
  await expect
    .poll(
      async () =>
        (await go<{ enabled: boolean }[]>("ListShopRules"))[0].enabled,
    )
    .toBe(false);
  const a11y = await new AxeBuilder({ page })
    .include("#shop-rules")
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(a11y.violations).toEqual([]);

  // Skills: file one under a category; the smart page then offers it.
  await page
    .getByRole("button", { name: "设置技能 生图提示词写法 的品类" })
    .click();
  const cat = page.getByRole("dialog", { name: /设置品类/ });
  await cat.getByLabel("品类").fill("马克杯");
  await cat.getByRole("button", { name: "保存" }).click();
  await expect(page.getByText("品类：马克杯")).toBeVisible();
  await page.getByRole("link", { name: "创作", exact: true }).click();
  await openMore();
  await page.getByText("更多设置", { exact: true }).click();
  await expect(page.getByLabel("商品品类（可选）")).toBeVisible();
  await go("SetSkillCategory", "image-prompting", "");
});
