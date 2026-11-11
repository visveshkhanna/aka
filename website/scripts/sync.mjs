// Copies files kept at the repository root into the site: CHANGELOG.md as
// the changelog page, and install.sh for the install command. The
// repository copies stay the one source of truth.
import { copyFileSync, readFileSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const root = new URL('../', import.meta.url);
const source = readFileSync(new URL('../CHANGELOG.md', root), 'utf8');

const body = source
  // the page has its own title and intro
  .replace(/^# Changelog\n+[^\n]*\n+/, '')
  // version headings link to their GitHub release
  .replace(/^## \[(\d+\.\d+\.\d+[^\]]*)\] - (\d{4}-\d{2}-\d{2})$/gm,
    (_, v, date) => `## [v${v}](https://github.com/visveshkhanna/aka/releases/tag/v${v}) · ${date}`)
  // drop the empty Unreleased section and the link definitions
  .replace(/^## \[Unreleased\]\n+(?=## )/m, '')
  .replace(/^## \[Unreleased\]$/m, '## Unreleased')
  .replace(/^\[[^\]]+\]: .*$/gm, '')
  .trimEnd();

const page = `---
title: Changelog
description: What changed in each release of aka.
---

{/* Generated from CHANGELOG.md by scripts/sync.mjs. Edit that file instead. */}

Releases, with downloads, are on [GitHub](https://github.com/visveshkhanna/aka/releases).

${body}
`;

const out = fileURLToPath(new URL('content/docs/changelog.mdx', root));
writeFileSync(out, page);
console.log(`wrote ${out}`);

// Serve the install script from the site: curl -fsSL https://aka.visvesh.dev/install.sh | sh
copyFileSync(new URL('../install.sh', root), new URL('public/install.sh', root));
console.log('copied install.sh to public/');
