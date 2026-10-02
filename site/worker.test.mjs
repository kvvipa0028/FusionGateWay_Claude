// node --test site/: the update feed's notes follow the app's language
// (freecss on Discord). GitHub and the edge cache are stood in for.
import { test } from "node:test";
import assert from "node:assert/strict";
import worker from "./worker.js";

const EN = "### Features\n\n- One thing (#1)\n\n### Install\n\nDownload it.";
const ZH = "### 新功能\n\n- 一件事 (#1)";
const BODIES = {
  "v0.1.3": `${EN}\n\n<!-- lang:zh -->\n\n${ZH}\n`,
  "v0.1.2": "### Fixes\n\n- Older, English alone",
  "v0.1.1": "- first",
};

const store = new Map();
globalThis.caches = {
  default: {
    match: async (req) => (store.has(req.url) ? new Response(store.get(req.url)) : undefined),
    put: async (req, res) => void store.set(req.url, await res.text()),
  },
};
globalThis.fetch = async (u) => {
  u = String(u);
  const gh = (tag) => ({ tag_name: tag, body: BODIES[tag], html_url: "https://github.com/r/" + tag, published_at: "2026-10-01T00:00:00Z", draft: false, prerelease: false, assets: [{ name: "magpie-linux-amd64", size: 1, browser_download_url: "https://dl/x" }] });
  if (u.endsWith("/releases/latest")) return Response.json(gh("v0.1.3"));
  if (u.includes("/releases?per_page=")) return Response.json(Object.keys(BODIES).map(gh));
  if (u.endsWith("/SHA256SUMS")) return new Response("abc  magpie-linux-amd64\n");
  return new Response("not found", { status: 404 });
};

const ctx = { waitUntil: (p) => p };
const get = async (path) => {
  const res = await worker.fetch(new Request("https://usemagpie.ai" + path), {}, ctx);
  assert.equal(res.status, 200, path);
  return { body: await res.json(), cc: res.headers.get("Cache-Control") };
};

test("latest: the English alone without lang, the Chinese with zh", async () => {
  for (const q of ["", "?lang=en", "?lang=fr"]) {
    const { body, cc } = await get("/api/latest" + q);
    assert.equal(body.version, "0.1.3");
    assert.equal(body.notes, EN, q);
    assert.match(cc, /max-age=/);
  }
  for (const q of ["?lang=zh", "?lang=zh-CN", "?lang=zh_Hans"]) {
    assert.equal((await get("/api/latest" + q)).body.notes, ZH, q);
  }
  // asked in Chinese first, the edge's copy still has both
  assert.equal((await get("/api/latest")).body.notes, EN);
});

test("notes: each release in the language, English where it has no Chinese", async () => {
  const zh = (await get("/api/notes?after=0.1.1&upto=0.1.3&lang=zh")).body.releases;
  assert.deepEqual(zh.map((r) => [r.version, r.notes]), [["0.1.3", ZH], ["0.1.2", BODIES["v0.1.2"]]]);
  const en = (await get("/api/notes?after=0.1.1&upto=0.1.3")).body.releases;
  assert.deepEqual(en.map((r) => [r.version, r.notes]), [["0.1.3", EN], ["0.1.2", BODIES["v0.1.2"]]]);
  const again = (await get("/api/notes?after=0.1.1&upto=0.1.3&lang=zh")).body.releases;
  assert.equal(again[0].notes, ZH);
});
