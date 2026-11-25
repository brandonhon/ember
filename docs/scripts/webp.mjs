// Derives a WebP copy of every screenshot PNG in public/screenshots/ for the
// home page (Landing.vue). Runs before `docs:dev` and `docs:build` via npm's
// pre-scripts. The PNGs stay the source of truth (the screenshot scripts
// write them, screenshots.md links them); the .webp files are build output,
// gitignored, and only re-encoded when their PNG is newer.
import { readdir, stat } from 'node:fs/promises';
import { join } from 'node:path';
import sharp from 'sharp';

const DIR = new URL('../public/screenshots/', import.meta.url).pathname;
// 2x the landing page's 1152px max content width, rounded down.
const MAX_WIDTH = 2000;

const mtime = (p) => stat(p).then((s) => s.mtimeMs, () => 0);

let made = 0;
for (const name of (await readdir(DIR)).filter((f) => f.endsWith('.png'))) {
  const src = join(DIR, name);
  const out = src.replace(/\.png$/, '.webp');
  if ((await mtime(out)) >= (await mtime(src))) continue;
  await sharp(src)
    .resize({ width: MAX_WIDTH, withoutEnlargement: true })
    .webp({ quality: 82 })
    .toFile(out);
  made++;
}
console.log(`webp: ${made} screenshot(s) encoded`);
