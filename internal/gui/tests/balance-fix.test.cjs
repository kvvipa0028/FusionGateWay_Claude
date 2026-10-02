// Run with Node's test runner and Playwright on the module path; see README.md.
// A custom provider's balance token beside new-api's /api/usage/token, which
// takes only the key, is said in the editor with the one click that moves it
// to /api/user/self (and its field to the quota); the New-Api-User header
// that wants is asked for until it is set, Check balance asks as the form
// has it, and the Usage page says the fix for either refusal plainly.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const relay = {
  id: "relay", name: "Relay", icon: "generic", chat: "https://relay.example.com/v1", responses: "", anthropic: "", catalog: "",
  models: [{ id: "model-a", name: "Model A", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…one" }, keyList: [],
  balanceURL: "https://relay.example.com/api/usage/token", balancePath: "$data.total_available / 500000",
  balanceToken: { takes: true, set: true },
};
const providers = { providers: [relay], presets: [], excluded: [], gateway: { running: true, window: true } };
const state = { agents: [], profiles: [], settings: { lang: "en", theme: "light" } };

function server(posted) {
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: 'window.bootPrefs = {lang:"en",theme:"light",web:true};' });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/")) {
      const body = route.request().postDataJSON();
      posted.push({ action: url.pathname.slice("/api/provider/".length), body });
      if (url.pathname === "/api/provider/balance") {
        // new-api's /api/user/self: the quota once New-Api-User is there
        if (body.headers?.["New-Api-User"]) return json({ ok: true, amount: "$3.00" });
        return json({ ok: true, amount: "", error: "401 Unauthorized: add the header New-Api-User = your user ID (shown in the site's personal settings) to this provider's Headers; the relay said: 无权进行此操作，未提供 New-Api-User" });
      }
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  test(engine + ": a balance token on /api/usage/token is moved to /api/user/self", async (t) => {
    assert(["chromium", "webkit"].includes(engine), "BROWSER must be chromium or webkit");
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const context = await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    const posted = [];
    await page.route("**/*", server(posted));
    t.after(async () => {
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, engine + "-balance-fix.png") });
      }
      await browser.close();
    });

    await page.goto("http://magpie.test/?view=providers");
    await page.locator(".row.provider", { hasText: "Relay" }).click();
    const fix = page.locator(".editor .bal-fix");
    await fix.waitFor();
    assert(await fix.evaluate((e) => e.classList.contains("warn")), "the saved token beside /api/usage/token is a warning");
    assert.match(await fix.textContent(), /\/api\/usage\/token takes the API key, not this token/);
    const use = fix.getByRole("button", { name: "Use https://relay.example.com/api/user/self" });
    await use.click();

    assert.equal(await page.locator(".editor .bal-url").inputValue(), "https://relay.example.com/api/user/self");
    assert.equal(await page.locator(".editor .bal-path").inputValue(), "$data.quota / 500000", "usage/token's field gives way to the quota");
    assert(await page.locator(".editor details.more").evaluate((d) => d.open), "what changed is in view");
    assert(!(await fix.evaluate((e) => e.classList.contains("warn"))));
    assert.match(await fix.textContent(), /New-Api-User = your user ID/, "the header /api/user/self wants is asked for");

    // asked as the form has it, without the header: the fix said plainly
    const check = page.getByRole("button", { name: "Check balance" });
    const res = page.locator(".editor .bal-res");
    await check.click();
    await page.locator(".editor .bal-res.bad").waitFor();
    assert.equal(await res.textContent(), "Add the header New-Api-User = your user ID (shown in the site's personal settings) to the provider's Headers");
    assert.match(await res.getAttribute("title"), /未提供 New-Api-User/, "the relay's own words on hover");
    let asked = posted.filter((p) => p.action === "balance").at(-1).body;
    assert.equal(asked.id, "relay");
    assert.equal(asked.balanceURL, "https://relay.example.com/api/user/self");
    assert.equal(asked.balancePath, "$data.quota / 500000");

    // the header typed in, the hint goes and the balance reads
    const row = page.locator(".editor .headers .pair").first();
    await row.locator("input").nth(0).fill("New-Api-User");
    await row.locator("input").nth(1).fill("42");
    assert.equal(await fix.textContent(), "", "no hint once New-Api-User is set");
    await check.click();
    await page.locator(".editor .bal-res.ok").waitFor();
    assert.equal(await res.textContent(), "Balance $3.00");
    asked = posted.filter((p) => p.action === "balance").at(-1).body;
    assert.deepEqual(asked.headers, { "New-Api-User": "42" });

    // Save keeps what the click set
    await page.locator(".editor .bar").getByRole("button", { name: "Save" }).click();
    await page.waitForTimeout(300);
    const saved = posted.filter((p) => p.action === "save").at(-1).body;
    assert.equal(saved.balanceURL, "https://relay.example.com/api/user/self");
    assert.equal(saved.balancePath, "$data.quota / 500000");
    assert.deepEqual(saved.headers, { "New-Api-User": "42" });

    // the Usage page's card says either fix plainly, in English and Chinese
    const said = await page.evaluate(() => [
      quotaError("the Balance URL https://relay.example.com/api/usage/token takes the API key, not the access token: set it to https://relay.example.com/api/user/self (Balance field $data.quota / 500000) for the account's balance, or remove the token for the key's own"),
      quotaError("401 Unauthorized: add the header New-Api-User = your user ID (shown in the site's personal settings) to this provider's Headers; the relay said: 无权进行此操作，未提供 New-Api-User"),
      quotaError("401 Unauthorized: something else"),
    ]);
    assert.match(said[0], /set it to …\/api\/user\/self/);
    assert.match(said[1], /^Add the header New-Api-User = your user ID/);
    assert.equal(said[2], "Allowance unavailable");
    const missing = await page.evaluate(() => [
      "…/api/usage/token takes the API key, not this token: a new-api relay tells the account's balance to the token at /api/user/self.",
      "The token needs a Balance URL: a new-api relay tells the account's balance to it at /api/user/self.",
      "Use {url}", "Check balance", "Ask the Balance URL now, as the form has it", "No Balance URL to ask",
      "A new-api relay also wants the header New-Api-User = your user ID (shown in the site's personal settings): add it under Headers.",
      "The Balance URL …/api/usage/token takes the API key, not the access token — set it to …/api/user/self in the provider's settings",
      "Add the header New-Api-User = your user ID (shown in the site's personal settings) to the provider's Headers",
    ].filter((k) => !I18N.zh[k]));
    assert.deepEqual(missing, [], "every new string has its Chinese");
    assert.deepEqual(errors, []);
  });

  test(engine + ": a token on a new custom provider with no Balance URL is pointed at /api/user/self", async (t) => {
    const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
    const context = await browser.newContext({ viewport: { width: 900, height: 760 }, reducedMotion: "reduce" });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    const errors = [];
    page.on("pageerror", (e) => errors.push(e.message));
    await page.route("**/*", server([]));
    t.after(() => browser.close());

    await page.goto("http://magpie.test/?view=providers");
    await page.locator("#providers .row.provider").first().waitFor();
    await page.evaluate(() => { adding = true; editing = { custom: true }; draft = null; renderProviders(); });
    const tok = page.locator(".editor input[type=password]").nth(1);
    await page.locator(".editor input[type=url]").first().fill("https://relay.example.com/v1");
    const fix = page.locator(".editor .bal-fix");
    assert.equal(await fix.textContent(), "", "no token, no hint");
    await tok.fill("access-token");
    assert.match(await fix.textContent(), /The token needs a Balance URL/);
    await fix.getByRole("button", { name: "Use https://relay.example.com/api/user/self" }).click();
    assert.equal(await page.locator(".editor .bal-url").inputValue(), "https://relay.example.com/api/user/self");
    assert.equal(await page.locator(".editor .bal-path").inputValue(), "$data.quota / 500000");
    assert.equal(await page.locator(".editor .bal-res").count(), 0, "a provider not yet added has nothing to check with");
    assert.deepEqual(errors, []);
  });
}
