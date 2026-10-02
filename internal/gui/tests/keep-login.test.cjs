// Run with Node's test runner and Playwright on the module path; see README.md.
// #524: under Codex's accounts, with more than one on, a tick keeps Codex
// signed in to the first account; ticking it posts provider/keeplogin, and
// the Routing note then says Codex stays on the first. #530: In order, the
// note says magpie moves Codex on once the account is used up, not at 98%.
// With one account there is nothing to keep, so no tick. The page doesn't
// move. In English and Chinese, Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const codex = ({ keepLogin = false, routing = "", spare = true } = {}) => ({
  id: "codex", name: "Codex", icon: "codex-color", chat: "", responses: "", anthropic: "", catalog: "", routing, keepLogin,
  models: [{ id: "gpt-6", name: "GPT-6", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: {
    agent: "codex", agentName: "Codex", user: "work@example.com", plan: "PLUS",
    logins: [
      { user: "work@example.com", plan: "PLUS", active: true, on: true },
      { user: "spare@example.com", plan: "PRO", on: spare },
    ],
  },
});

function serve(lang, first, posts) {
  let list = [first];
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  const providers = () => ({ providers: list, presets: [], excluded: [], gateway: { running: true, window: true } });
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers());
    if (url.pathname === "/api/provider/keeplogin") {
      const body = route.request().postDataJSON();
      posts.push(body);
      list = [{ ...list[0], keepLogin: body.keepLogin }];
      return json(providers());
    }
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const words = {
  en: { keep: "Keep Codex signed in to the first account", kept: /Codex on its own stays signed in to the first account/, moves: /once it is 98% used/, usedUp: /once it is used up/ },
  zh: { keep: "Codex 始终登录首选账号", kept: /Codex 自己直连时始终登录首选账号/, moves: /用到 98% 时/, usedUp: /额度用完时/ },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh"]) {
    const w = words[lang];
    const open = async (t, opts) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      const errors = [], posts = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", serve(lang, codex(opts), posts));
      await page.goto("http://magpie.test/?view=providers");
      await page.locator(".row.provider", { hasText: "Codex" }).first().click();
      await page.locator(".editor .acc").first().waitFor();
      return { page, errors, posts };
    };

    test(`${engine} ${lang}: a tick keeps Codex signed in to the first account`, async (t) => {
      const { page, errors, posts } = await open(t, {});
      const keep = page.locator(".editor .accts label.keep-login");
      assert.equal((await keep.textContent()).trim(), w.keep);
      assert.ok(await keep.getAttribute("title"), "it says what it does");
      assert.equal(await keep.locator("input").isChecked(), false);
      assert.match(await page.locator(".editor").textContent(), w.moves);
      const missing = await page.evaluate(() => [
        "Keep {agent} signed in to the first account",
        "magpie won't sign {agent} in to another account when the first runs low; requests through magpie still go to the other ticked accounts as Routing says",
        "{agent} stays signed in to the first account",
        "magpie moves {agent} to an account with room again",
        "Routing picks the account for each request through magpie; {agent} on its own stays signed in to the first account, whatever it has left.",
        "Routing picks the account for each request through magpie; {agent} on its own uses the one it is signed in to, which magpie moves to the next ticked account with room once it is used up, and back to the first once that has room again.",
      ].filter((k) => !I18N.zh[k]));
      assert.deepEqual(missing, [], "every string has its Chinese");
      const top = await page.evaluate(() => document.scrollingElement.scrollTop);
      await keep.locator("input").click();
      // the answer drawn: the note says so, and the box is ticked
      const says = (re) => page.waitForFunction((src) => new RegExp(src).test(document.querySelector(".editor")?.textContent || ""), re.source);
      await says(w.kept);
      assert.equal(await page.locator(".editor .accts label.keep-login input").isChecked(), true);
      assert.deepEqual(posts, [{ id: "codex", keepLogin: true }]);
      assert.match(await page.locator(".editor").textContent(), w.kept);
      assert.doesNotMatch(await page.locator(".editor").textContent(), w.moves);
      assert.equal(await page.evaluate(() => document.scrollingElement.scrollTop), top, "the page doesn't move");
      await page.locator(".editor .accts label.keep-login input").click();
      await says(w.moves);
      assert.equal(await page.locator(".editor .accts label.keep-login input").isChecked(), false);
      assert.deepEqual(posts[1], { id: "codex", keepLogin: false });
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: in order, Codex is moved on once the account is used up`, async (t) => {
      const { page, errors } = await open(t, { routing: "order" });
      const text = await page.locator(".editor").textContent();
      assert.match(text, w.usedUp);
      assert.doesNotMatch(text, w.moves);
      assert.deepEqual(errors, []);
    });

    test(`${engine} ${lang}: with one account on, there is nothing to keep`, async (t) => {
      const { page, errors } = await open(t, { spare: false });
      assert.equal(await page.locator(".editor .accts label.keep-login").count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}
