// Render the Symmetry SEO/Open Graph images from the HTML designs in
// docs/assets/seo. Each page is captured at 2x device scale and downsampled
// with a high-quality filter so text and edges stay crisp at 1x.
//
// Usage (from the repository root):
//   node scripts/render-seo-assets.mjs
//
// Playwright is resolved from browser/node_modules (run `npm ci` there), the
// root node_modules, or the global npm root, in that order. The downscale step
// needs Python 3 with Pillow.
import { createRequire } from "node:module";
import { mkdir, rm } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const seoDir = path.join(repoRoot, "docs", "assets", "seo");
const outDir = path.join(repoRoot, "docs", "assets");
const tmpDir = path.join(seoDir, ".tmp");

function resolvePlaywright() {
  const require = createRequire(import.meta.url);
  const candidates = [path.join(repoRoot, "browser", "node_modules"), path.join(repoRoot, "node_modules")];
  try {
    candidates.push(execFileSync("npm", ["root", "-g"], { encoding: "utf8" }).trim());
  } catch {
    // Global npm root is optional.
  }
  for (const base of candidates) {
    for (const name of ["@playwright/test", "playwright"]) {
      try {
        return require(require.resolve(name, { paths: [base] }));
      } catch {
        // Try the next candidate.
      }
    }
  }
  throw new Error("Playwright not found. Run `npm ci` in browser/ or install the playwright package.");
}

const { chromium } = resolvePlaywright();

const targets = [
  { html: "banner.html", out: "symmetry-banner.png", width: 1600, height: 400 },
  { html: "social-preview.html", out: "symmetry-social-preview.png", width: 1280, height: 640 },
];

const scale = 2;

async function launchBrowser() {
  try {
    return await chromium.launch();
  } catch (error) {
    // Fall back to a system Chrome install when the bundled Playwright build
    // is not downloaded (for example on a machine that only has Chrome).
    try {
      return await chromium.launch({ channel: "chrome" });
    } catch {
      throw error;
    }
  }
}

await mkdir(tmpDir, { recursive: true });
const browser = await launchBrowser();

for (const target of targets) {
  const page = await browser.newPage({
    viewport: { width: target.width, height: target.height },
    deviceScaleFactor: scale,
  });
  await page.goto(`file://${path.join(seoDir, target.html)}`, { waitUntil: "load" });
  await page.evaluate(() => document.fonts.ready);
  const raw = path.join(tmpDir, `${target.out}.2x.png`);
  await page.screenshot({ path: raw, clip: { x: 0, y: 0, width: target.width, height: target.height } });
  await page.close();

  // Supersampled downscale to the exact 1x target size.
  const out = path.join(outDir, target.out);
  execFileSync("python3", [
    "-c",
    [
      "import sys",
      "from PIL import Image",
      "src, dst, w, h = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])",
      "im = Image.open(src).convert('RGB')",
      "im = im.resize((w, h), Image.LANCZOS)",
      "im.save(dst, 'PNG', optimize=True)",
    ].join("\n"),
    raw,
    out,
    String(target.width),
    String(target.height),
  ]);
  console.log(`wrote ${path.relative(repoRoot, out)} (${target.width}x${target.height})`);
}

await browser.close();
await rm(tmpDir, { recursive: true, force: true });
