import { createGetUrl } from 'fumadocs-core/source';

export const appName = 'aka';
export const siteUrl = 'https://aka.visvesh.dev';
export const tagline = 'Stable, named .localhost URLs for every local dev server.';
export const description =
  'aka gives any local dev server a stable https://<name>.localhost URL, in any language or framework, and works out the port by itself.';
export const docsRoute = '/docs';
export const docsImageRoute = '/og/docs';
export const docsContentRoute = '/llms.mdx/docs';

export const gitConfig = {
  user: 'visveshkhanna',
  repo: 'aka',
  branch: 'main',
  // where the pages live in the repository
  contentDir: 'website/content/docs',
};

export const githubUrl = `https://github.com/${gitConfig.user}/${gitConfig.repo}`;

const getContentUrl = createGetUrl(docsContentRoute);

export function getPageMarkdownUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, 'content.md'];

  return { segments, url: getContentUrl(segments, page.locale) };
}

const getImageUrl = createGetUrl(docsImageRoute);

export function getPageImageUrl(page: { slugs: string[]; locale?: string }) {
  const segments = [...page.slugs, 'image.png'];

  return { segments, url: getImageUrl(segments, page.locale) };
}
