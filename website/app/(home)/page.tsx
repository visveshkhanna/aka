import Link from 'next/link';
import type { ReactNode } from 'react';
import { ArrowRight, Bot, Boxes, Layers, Lock, Radar, Terminal } from 'lucide-react';
import { CopyCommand } from '@/components/copy-command';
import { githubUrl } from '@/lib/shared';

const install = 'curl -fsSL https://aka.visvesh.dev/install.sh | sh';

const features: { icon: ReactNode; title: string; body: string; href: string }[] = [
  {
    icon: <Terminal className="size-5" />,
    title: 'Any stack',
    body: 'Node, Python, Ruby, PHP, Go, Rust, Java, .NET, Elixir. If it starts a server, aka gives it a URL.',
    href: '/docs/running-apps',
  },
  {
    icon: <Radar className="size-5" />,
    title: 'Finds the port',
    body: 'Apps get a free PORT. Presets cover servers that ignore it, and detection finds the rest.',
    href: '/docs/presets',
  },
  {
    icon: <Lock className="size-5" />,
    title: 'HTTPS by default',
    body: 'HTTP/2 from a local certificate authority that can only sign .localhost and loopback names.',
    href: '/docs/https',
  },
  {
    icon: <Layers className="size-5" />,
    title: 'Many apps at once',
    body: 'Two Django apps, two Angular apps, a whole aka.toml or a Docker Compose project, side by side.',
    href: '/docs/multiple-apps',
  },
  {
    icon: <Bot className="size-5" />,
    title: 'Built for agents',
    body: 'An MCP server lets coding agents find the URL of the app they work on instead of guessing a port.',
    href: '/docs/ai-agents',
  },
  {
    icon: <Boxes className="size-5" />,
    title: 'One static binary',
    body: 'macOS, Linux and Windows, on x64 and arm64. No runtime to install, nothing written into your projects.',
    href: '/docs/platforms',
  },
];

function TerminalDemo() {
  const lines: { text: string; tone?: 'cmd' | 'aka' | 'app' }[] = [
    { text: '$ aka web npm run dev', tone: 'cmd' },
    { text: '[aka] preset vite: --port 4217 --strictPort --host 127.0.0.1', tone: 'aka' },
    { text: '[aka] https://web.localhost:1355 -> npm run dev (PORT=4217)', tone: 'aka' },
    { text: '  VITE ready in 312 ms', tone: 'app' },
    { text: '[aka] ready: https://web.localhost:1355 -> 127.0.0.1:4217 (assigned PORT)', tone: 'aka' },
    { text: '' },
    { text: '$ aka api uvicorn main:app --reload', tone: 'cmd' },
    { text: '[aka] preset uvicorn: --port 4381 --host 127.0.0.1', tone: 'aka' },
    { text: '[aka] ready: https://api.localhost:1355 -> 127.0.0.1:4381 (assigned PORT)', tone: 'aka' },
  ];
  const tone = {
    cmd: 'text-fd-foreground',
    aka: 'text-fd-muted-foreground',
    app: 'text-fd-muted-foreground/70',
  } as const;
  return (
    <div className="w-full min-w-0 max-w-3xl overflow-hidden rounded-xl border bg-fd-card text-left shadow-lg">
      <div className="flex items-center gap-1.5 border-b px-4 py-3">
        <span className="size-3 rounded-full bg-fd-border" />
        <span className="size-3 rounded-full bg-fd-border" />
        <span className="size-3 rounded-full bg-fd-border" />
      </div>
      <pre className="overflow-x-auto p-5 text-[13px] leading-6">
        {lines.map((l, i) => (
          <div key={i} className={l.tone ? tone[l.tone] : undefined}>
            {l.text || ' '}
          </div>
        ))}
      </pre>
    </div>
  );
}

export default function HomePage() {
  return (
    <main className="flex flex-1 flex-col overflow-x-clip">
      <section className="relative flex flex-col items-center gap-8 px-4 pb-16 pt-20 text-center md:pt-28">
        <div aria-hidden className="hero-grid pointer-events-none absolute inset-0 -z-10" />
        <Link
          href="/docs/changelog"
          className="rounded-full border bg-fd-card px-3 py-1 text-xs text-fd-muted-foreground transition-colors hover:text-fd-foreground"
        >
          Open source · macOS, Linux and Windows
        </Link>
        <h1 className="max-w-3xl text-4xl font-semibold tracking-tighter md:text-6xl">
          Stable URLs for every local dev server
        </h1>
        <p className="max-w-2xl text-lg text-fd-muted-foreground">
          aka gives any dev server a named <code className="font-mono text-fd-foreground">https://&lt;name&gt;.localhost</code>{' '}
          URL, in any language or framework, and works out the port by itself.
        </p>
        <CopyCommand command={install} />
        <div className="flex flex-wrap items-center justify-center gap-3">
          <Link
            href="/docs"
            className="inline-flex items-center gap-2 rounded-lg bg-fd-foreground px-5 py-2.5 text-sm font-medium text-fd-background transition-opacity hover:opacity-90"
          >
            Get started <ArrowRight className="size-4" />
          </Link>
          <a
            href={githubUrl}
            className="inline-flex items-center gap-2 rounded-lg border bg-fd-card px-5 py-2.5 text-sm font-medium transition-colors hover:bg-fd-accent"
          >
            GitHub
          </a>
        </div>
        <TerminalDemo />
      </section>

      <section className="mx-4 grid max-w-5xl gap-px overflow-hidden rounded-xl border bg-fd-border sm:grid-cols-2 lg:mx-auto lg:w-full lg:grid-cols-3">
        {features.map((f) => (
          <Link key={f.title} href={f.href} className="group flex flex-col gap-3 bg-fd-background p-6 transition-colors hover:bg-fd-card">
            <span className="text-fd-muted-foreground transition-colors group-hover:text-fd-foreground">{f.icon}</span>
            <h2 className="font-semibold">{f.title}</h2>
            <p className="text-sm text-fd-muted-foreground">{f.body}</p>
          </Link>
        ))}
      </section>

      <section className="mx-auto flex w-full max-w-3xl flex-col items-center gap-4 px-4 py-20 text-center">
        <h2 className="text-2xl font-semibold tracking-tight">Replace port numbers with names</h2>
        <p className="text-fd-muted-foreground">
          The URL stays the same across restarts, so bookmarks, cookies, OAuth redirects and test scripts keep pointing at
          the right app.
        </p>
        <pre className="w-full overflow-x-auto rounded-lg border bg-fd-card p-4 text-left text-sm">
          <span className="text-red-600 dark:text-red-400">- python manage.py runserver        # http://127.0.0.1:8000</span>
          {'\n'}
          <span className="text-green-700 dark:text-green-400">+ aka python manage.py runserver    # https://myapp.localhost:1355</span>
        </pre>
      </section>

      <footer className="border-t px-4 py-8 text-center text-sm text-fd-muted-foreground">
        Apache-2.0 licensed. Inspired by{' '}
        <a href="https://github.com/vercel-labs/portless" className="underline underline-offset-4 hover:text-fd-foreground">
          portless
        </a>
        .
      </footer>
    </main>
  );
}
