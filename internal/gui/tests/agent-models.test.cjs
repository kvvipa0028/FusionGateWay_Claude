// Run with Node's test runner and Playwright on the module path; see README.md.
// An agent's model list (@ChongkaiX on X: pick which models show in Codex's
// /model): the line under its name counts them and opens the list, where a
// click on a row takes a model out or puts it back, at once.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const models = () => [
  { id: "group:fast", name: "fast", group: "Routing groups", icons: ["openai", "anthropic"] },
  ...["gpt-5.5", "gpt-5.5-mini", "gpt-5.4", "gpt-5.3-codex", "o4", "o4-mini"].map((m, i) =>
    ({ id: "openai/" + m, name: m.toUpperCase(), group: "OpenAI", icon: "openai", context: i ? 400e3 : 1e6, inUse: i === 0 })),
  ...Array.from({ length: 24 }, (_, i) => ({ id: "openrouter/m" + i, name: "Router Model " + i, group: "OpenRouter", icon: "openrouter", context: 128e3, hidden: i > 0 })),
];

function fixture(lang) {
  const list = models();
  const posts = [];
  const count = () => ({ shown: list.filter((m) => !m.hidden).length, listed: list.length });
  const state = () => ({
    agents: [{
      id: "codex", name: "Codex", path: "/test/config.toml", icon: "codex-color",
      fields: [{ key: "model", label: "model", value: "magpie/openai/gpt-5.5", options: [{ value: "magpie/openai/gpt-5.5", label: "GPT-5.5", ref: "openai/gpt-5.5" }] }],
      models: count(),
    }],
    profiles: [], settings: { lang, theme: "light" },
  });
  async function serve(route) {
    const req = route.request();
    const url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state());
    if (url.pathname === "/api/agent-models/codex") {
      if (req.method() === "POST") {
        const { hidden } = req.postDataJSON();
        posts.push(hidden);
        for (const m of list) m.hidden = hidden.includes(m.id) && !m.inUse;
        return json({ models: list, count: count() });
      }
      return json({ models: list });
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], gateway: { running: true, window: true } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  }
  return { serve, posts };
}

const W = {
  en: { entry: "Showing 8 / 31 models", after: "Showing 7 / 31 models", all: "All 31 models", title: "Codex's model list", current: "Current", shown: "Shown", hideAll: "Hide all", one: "Showing 1 / 31 models", showAll: "Show all" },
  zh: { entry: "显示 8 / 31 个模型", after: "显示 7 / 31 个模型", all: "全部 31 个模型", title: "Codex 的模型列表", current: "在用", shown: "已显示", hideAll: "全部隐藏", one: "显示 1 / 31 个模型", showAll: "全部显示" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    test(`${engine} ${lang}: an agent's model list`, async (t) => {
      const w = W[lang];
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      const page = await browser.newPage({ viewport: { width: 1000, height: 700 } });
      page.setDefaultTimeout(5000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const fx = fixture(lang);
      await page.route("**/*", fx.serve);
      t.after(async () => {
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-models.png`) });
        }
        await browser.close();
      });
      await page.goto("http://magpie.test/");
      const entry = page.locator('.row.agent[data-id="codex"] .ag-models');
      await entry.waitFor();
      assert.equal((await entry.innerText()).trim(), w.entry);
      // under the name, not beside it
      const name = await page.locator('.row.agent[data-id="codex"] .who .name').boundingBox();
      const eb = await entry.boundingBox();
      assert(eb.y >= name.y + name.height - 2, "the line is under the name");

      const scroll = () => page.evaluate(() => [scrollY, document.scrollingElement.scrollTop, $("#view-agents").scrollTop]);
      const was = await scroll();
      await entry.click();
      const pop = page.locator(".am-pop:not(.leaving)");
      await pop.waitFor();
      assert.deepEqual(await scroll(), was, "opening it moves nothing");
      assert.equal(await pop.locator(".am-t").innerText(), w.title);
      // on the screen whole
      const pb = await pop.boundingBox();
      assert(pb.x >= 0 && pb.y >= 0 && pb.x + pb.width <= 1000 && pb.y + pb.height <= 700, JSON.stringify(pb));
      assert(await page.evaluate(() => document.activeElement?.closest(".am-search") !== null), "the search has the keys");

      // routing groups first; the long OpenRouter folded, OpenAI open
      const groups = await pop.locator(".am-fold .gn").allInnerTexts();
      assert.equal(groups.length, 3);
      assert.equal(groups[1], "OpenAI");
      assert.equal(groups[2], "OpenRouter");
      assert.equal(await pop.locator(".am-g").nth(2).locator(".am-mr").count(), 0, "OpenRouter's 24 start folded");
      assert.equal(await pop.locator(".am-g").nth(1).locator(".am-mr").count(), 6);
      assert.equal(await pop.locator(".am-g").nth(2).locator(".c").innerText(), "1 / 24");

      // the one Codex is set to stays
      const cur = pop.locator(".am-mr", { hasText: "GPT-5.5" }).first();
      assert.equal(await cur.locator(".am-tag").innerText(), w.current);
      await cur.click({ force: true }); // aria-disabled: Playwright waits for it to be enabled
      assert.equal(fx.posts.length, 0, "the model in use can't be taken out");
      assert.equal(await cur.locator(".ck svg").count(), 1);

      // a click takes one out at once, the count under the name with it
      const mini = pop.locator(".am-mr", { hasText: "O4-MINI" });
      await mini.click();
      await page.waitForFunction(() => document.querySelector(".ag-models")?.innerText.includes("7"));
      assert.equal((await entry.innerText()).trim(), w.after);
      await page.waitForTimeout(150);
      assert.equal(fx.posts.length, 1);
      assert(fx.posts[0].includes("openai/o4-mini") && fx.posts[0].includes("openrouter/m5"), "the whole list goes");
      assert.equal(await mini.locator(".ck svg").count(), 0);
      assert(await mini.evaluate((e) => e.classList.contains("off")));
      assert.equal(await pop.locator(".am-g").nth(1).locator(".c").innerText(), "5 / 6");

      // a search opens a folded group to what it finds
      const q = pop.locator(".am-search input");
      await q.fill("router model 7");
      assert.equal(await pop.locator(".am-mr").count(), 1);
      await q.fill("");

      // "Shown": only those shown, one turned off there stays till it changes
      await pop.locator(".am-seg button", { hasText: w.shown }).click();
      assert.equal(await pop.locator(".am-mr").count(), 1 + 5 + 0, "routing group, OpenAI's 5; OpenRouter folded");
      await pop.locator(".am-mr", { hasText: "O4" }).first().click();
      assert.equal(await pop.locator(".am-mr").count(), 6);

      // the group's "show all" comes on hover
      const gh = pop.locator(".am-gh").nth(2);
      assert.equal(await gh.locator(".am-all").evaluate((e) => getComputedStyle(e).opacity), "0");
      await gh.hover();
      await page.waitForTimeout(250);
      assert.equal(await gh.locator(".am-all").evaluate((e) => getComputedStyle(e).opacity), "1");
      assert.equal(await gh.locator(".am-all").innerText(), w.showAll);

      // every one hidden at once, but the one in use
      const hideAll = pop.locator(".am-hide"), showAll = pop.locator(".am-reset:not(.am-hide)");
      assert.equal(await hideAll.innerText(), w.hideAll);
      assert.equal(await showAll.innerText(), w.showAll);
      await hideAll.click();
      await page.waitForTimeout(150);
      assert.equal(fx.posts.at(-1).length, 30);
      assert(!fx.posts.at(-1).includes("openai/gpt-5.5"));
      assert.equal((await entry.innerText()).trim(), w.one);
      assert(await hideAll.isDisabled());
      assert.equal(await pop.locator(".am-g").nth(1).locator(".c").innerText(), "1 / 6");

      // back to all shown
      await showAll.click();
      await page.waitForTimeout(150);
      assert.deepEqual(fx.posts.at(-1), []);
      assert.equal((await entry.innerText()).trim(), w.all);
      assert(await showAll.isDisabled());
      assert(!(await hideAll.isDisabled()));

      // Esc closes it; so does a click elsewhere
      await page.keyboard.press("Escape");
      await pop.waitFor({ state: "detached" });
      await entry.click();
      await pop.waitFor();
      await page.mouse.click(900, 650);
      await pop.waitFor({ state: "detached" });
      assert.deepEqual(errors, []);
    });
  }
}
