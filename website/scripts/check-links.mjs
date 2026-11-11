// Checks every internal link and #anchor in the built site (out/), so a
// renamed page or heading fails CI instead of shipping a broken link.
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const out = new URL('../out/', import.meta.url).pathname;
if (!existsSync(out)) {
  console.error('out/ not found: run `npm run build` first');
  process.exit(1);
}

const pages = [];
(function walk(dir) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p);
    else if (p.endsWith('.html')) pages.push(p);
  }
})(out);

// The file a URL path is served from, as Workers static assets resolve it.
function fileFor(path) {
  const clean = decodeURIComponent(path.replace(/\/$/, '')) || '/index';
  for (const candidate of [clean + '.html', join(clean, 'index.html'), clean]) {
    const file = join(out, candidate);
    if (existsSync(file) && statSync(file).isFile()) return file;
  }
  return null;
}

const ids = new Map();
function idsOf(file) {
  if (!ids.has(file)) {
    const html = readFileSync(file, 'utf8');
    ids.set(file, new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1])));
  }
  return ids.get(file);
}

let broken = 0;
for (const page of pages) {
  const html = readFileSync(page, 'utf8');
  for (const [, href] of html.matchAll(/<a\s[^>]*href="([^"]+)"/g)) {
    if (/^(https?:|mailto:|#$)/.test(href)) continue;
    const [path, hash] = href.split('#');
    const target = path ? fileFor(path) : page;
    if (!target) {
      console.error(`${relative(out, page)}: broken link ${href}`);
      broken++;
    } else if (hash && target.endsWith('.html') && !idsOf(target).has(decodeURIComponent(hash))) {
      console.error(`${relative(out, page)}: missing anchor ${href}`);
      broken++;
    }
  }
}
console.log(`checked ${pages.length} pages: ${broken} broken links`);
process.exit(broken ? 1 : 0);
