import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { appName, githubUrl } from './shared';

export function Logo() {
  return (
    <span className="inline-flex items-center gap-2 font-mono text-[15px] font-semibold tracking-tight">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src="/brand/logo-64.png" alt="" width={22} height={22} className="rounded-[5px]" />
      <span>
        {appName}
        <span className="text-fd-muted-foreground">.localhost</span>
      </span>
    </span>
  );
}

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: <Logo />,
    },
    githubUrl,
    links: [
      { text: 'Docs', url: '/docs', active: 'nested-url' },
      { text: 'Changelog', url: '/docs/changelog' },
    ],
  };
}
