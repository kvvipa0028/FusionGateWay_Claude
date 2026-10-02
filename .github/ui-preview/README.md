# UI preview

`.github/workflows/ui-preview.yml` shows reviewers what a pull request does to the UI.
When a PR touches `internal/gui/assets/` or the GUI's Go code, or carries the `ui-preview` label, the workflow does this:

1. **publish.mjs detect** decides whether the PR is a UI change. It puts a "recording…" note in a fixed block at the foot of the PR description, between `<!-- magpie-ui-preview:begin -->` and `<!-- magpie-ui-preview:end -->`.
2. **run.sh** builds the PR's magpie (nogui) and runs `magpie web` in a fresh home, with DeepSeek added as a real provider. It sends a few requests through the gateway so Routing and Usage aren't empty, then runs:
3. **record.mjs**, which:
   - gives DeepSeek the diff and outlines of the real pages;
   - has it plan scenes: what to click, hover, type and capture;
   - walks the plan in Chromium at 2x, with `cursor.js` drawing the pointer, its trail and each click.
   It always takes screenshots. When the change spans pages or needs interaction, it also records an mp4, never a GIF. If the key shows up on screen at any point, everything is thrown away.
4. **publish.mjs publish** puts the files under `pr-<n>/<sha>/` on the `ui-previews` branch, which GitHub Pages serves, and rewrites the block in the description:
   - the video's poster links to the player page, since GitHub won't embed a video it didn't host;
   - the screenshots are shown inline.

Security: the recording job runs the PR's code without asking anyone. Any PR's code can therefore read `DEEPSEEK_API_KEY` (environment `ui-preview`), so the key there should have a low spending limit. That job has no write access, and the job that writes never runs the PR's code.

To run it locally (run.sh builds and starts magpie itself):

```sh
cd .github/ui-preview && npm install && npx playwright install chromium
DEEPSEEK_API_KEY=… DIFF_FILE=pr.diff PR_TITLE=… ./run.sh ../.. /tmp/out
```

To re-run it for a PR, use `gh workflow run ui-preview.yml -f pr=<n>`.
