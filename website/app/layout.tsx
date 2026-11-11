import type { Metadata } from 'next';
import { GeistSans } from 'geist/font/sans';
import { GeistMono } from 'geist/font/mono';
import { Provider } from '@/components/provider';
import { appName, description, siteUrl, tagline } from '@/lib/shared';
import './global.css';

export const metadata: Metadata = {
  metadataBase: new URL(siteUrl),
  title: {
    default: `${appName}: ${tagline}`,
    template: `%s | ${appName}`,
  },
  description,
  openGraph: { siteName: appName, type: 'website' },
  twitter: { card: 'summary_large_image' },
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
