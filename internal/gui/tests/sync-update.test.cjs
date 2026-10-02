// Run with Node's test runner and Playwright on the module path; see README.md.
// The header's refresh button looks for a newer magpie as well as refreshing
// the model lists: one click asks the backend to check, and the Update pill
// beside it shows what the check found. Its tooltip says so. English and
// Chinese; no backend, the API is faked here.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");

const words = {
  en: { tip: "Refresh model lists (models.dev and every vendor) and check for a newer magpie", update: "Update" },
  zh: { tip: "刷新模型列表（models.dev 与各供应商），并检查 magpie 更新", update: "更新" },
};

function server(lang, ctl) {
  return async (route) => {
    const req = route.request(), url = new URL(req.url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:false};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state" || url.pathname === "/api/sync") {
      if (url.pathname === "/api/sync") ctl.syncs++;
      return json({ agents: [], profiles: [], settings: { lang, theme: "light" } });
    }
    if (url.pathname === "/api/update/check") {
      ctl.checks++;
      ctl.update = { state: "ready", current: "0.1.400", latest: "0.1.401" };
      return json(ctl.update);
    }
    if (url.pathname === "/api/update") return json(ctl.update);
    if (url.pathname === "/api/usage/quotas") return json([]);
    if (url.pathname === "/api/groups") return json({ groups: [], models: [] });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": the refresh button checks for updates", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    t.after(() => browser.close());
    for (const lang of ["en", "zh"]) {
      await t.test(lang, async () => {
        const ctl = { syncs: 0, checks: 0, update: { state: "latest", current: "0.1.400" } };
        const page = await (await browser.newContext({ viewport: { width: 1000, height: 600 } })).newPage();
        page.setDefaultTimeout(5000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        await page.route("**/*", server(lang, ctl));
        await page.goto("http://magpie.test/");
        const sync = page.locator("#sync"), pill = page.locator("#update");
        await sync.waitFor();
        assert.equal(await sync.getAttribute("title"), words[lang].tip);
        await page.waitForTimeout(300);
        assert.equal(await pill.isHidden(), true, "no update yet");
        const before = ctl.checks;
        await sync.click();
        await pill.waitFor({ state: "visible" });
        assert.equal(ctl.syncs, 1);
        assert.equal(ctl.checks - before, 1, "one click, one check");
        assert.equal((await pill.innerText()).trim(), words[lang].update);
        assert.deepEqual(errors, []);
      });
    }
  });
}
