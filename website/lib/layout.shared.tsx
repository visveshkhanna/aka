import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { appName, githubUrl } from './shared';

export function Logo() {
  return (
    <span className="font-mono text-[15px] font-semibold tracking-tight">
      {appName}
      <span className="text-fd-muted-foreground">.localhost</span>
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
