import type { Metadata, Viewport } from 'next';
import { GeistSans } from 'geist/font/sans';
import { GeistMono } from 'geist/font/mono';
import { Provider } from '@/components/provider';
import { appName, description, siteUrl, tagline } from '@/lib/shared';
import './global.css';

const homeImage = {
  url: '/og/home.png',
  width: 1200,
  height: 630,
  alt: `${appName}: stable, named .localhost URLs for every local dev server`,
};

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: {
    default: `${appName}: ${tagline}`,
    template: `%s | ${appName}`,
  },
  description,
  applicationName: appName,
  alternates: { canonical: '/' },
  openGraph: {
    siteName: appName,
    type: 'website',
    locale: 'en_US',
    url: '/',
    title: `${appName}: ${tagline}`,
    description,
    images: [homeImage],
  },
  twitter: {
    card: 'summary_large_image',
    title: `${appName}: ${tagline}`,
    description,
    images: [homeImage],
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: '(prefers-color-scheme: light)', color: '#ffffff' },
    { media: '(prefers-color-scheme: dark)', color: '#0a0a0a' },
  ],
};

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html
      lang="en"
      className={`${GeistSans.variable} ${GeistMono.variable}`}
      suppressHydrationWarning
    >
      <body className="flex min-h-screen flex-col font-sans antialiased">
        <Provider>{children}</Provider>
      </body>
    </html>
  );
}
