import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { ImageResponse } from 'next/og';
import type { ReactNode } from 'react';

// Open Graph cards, rendered at build time: the brand artwork, with the
// page's title and description in Geist on its empty left side. The
// artwork starts at about x = 640, so text stays within 72 + 540 px.

export const ogSize = { width: 1200, height: 630 };

const root = process.cwd();
const dataUrl = (file: string, type: string) =>
  `data:${type};base64,${readFileSync(join(root, file)).toString('base64')}`;
const font = (file: string) => readFileSync(join(root, 'node_modules/geist/dist/fonts', file));

const background = dataUrl('assets/og-background.jpg', 'image/jpeg');
const logo = dataUrl('public/brand/logo-64.png', 'image/png');
const fonts = [
  { name: 'Geist', data: font('geist-sans/Geist-Regular.ttf'), weight: 400 as const },
  { name: 'Geist', data: font('geist-sans/Geist-SemiBold.ttf'), weight: 600 as const },
  { name: 'Geist Mono', data: font('geist-mono/GeistMono-Regular.ttf'), weight: 400 as const },
];

function clip(text: string, max: number) {
  return text.length <= max ? text : `${text.slice(0, max - 1).trimEnd()}…`;
}

export function ogImage({
  title,
  description,
  eyebrow,
  footer,
  titleSize,
}: {
  title: string;
  titleSize?: number;
  description?: string;
  eyebrow?: string;
  footer?: ReactNode;
}) {
  const long = title.length > 28;
  return new ImageResponse(
    (
      <div
        style={{
          width: '100%',
          height: '100%',
          display: 'flex',
          flexDirection: 'column',
          justifyContent: 'space-between',
          padding: '64px 72px',
          backgroundImage: `url(${background})`,
          backgroundSize: '1200px 630px',
          color: '#fafafa',
          fontFamily: 'Geist',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={logo} width={44} height={44} alt="" />
          <div style={{ display: 'flex', fontFamily: 'Geist Mono', fontSize: 28 }}>
            <span>aka</span>
            <span style={{ color: '#8a8a8a' }}>.localhost</span>
          </div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', maxWidth: 540, gap: 20 }}>
          {eyebrow ? (
            <div
              style={{
                display: 'flex',
                fontFamily: 'Geist Mono',
                fontSize: 22,
                color: '#8a8a8a',
                textTransform: 'uppercase',
                letterSpacing: 2,
              }}
            >
              {eyebrow}
            </div>
          ) : null}
          <div
            style={{
              display: 'flex',
              fontSize: titleSize ?? (long ? 60 : 72),
              fontWeight: 600,
              lineHeight: 1.05,
              letterSpacing: -2,
            }}
          >
            {title}
          </div>
          {description ? (
            <div style={{ display: 'flex', fontSize: 28, lineHeight: 1.4, color: '#a3a3a3' }}>
              {clip(description, 130)}
            </div>
          ) : null}
        </div>

        <div style={{ display: 'flex', alignItems: 'center', fontFamily: 'Geist Mono', fontSize: 22, color: '#8a8a8a' }}>
          {footer ?? 'aka.visvesh.dev'}
        </div>
      </div>
    ),
    { ...ogSize, fonts },
  );
}
