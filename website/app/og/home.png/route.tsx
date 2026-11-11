import { ogImage } from '@/lib/og';

// The card for the home page, and the default for pages without their own.
// A route with a .png path, so it is served as image/png.
export const dynamic = 'force-static';

export function GET() {
  return ogImage({
    title: 'Stable URLs for every local dev server',
    titleSize: 54,
    description: 'Any language or framework. aka works out the port by itself.',
    footer: (
      <div
        style={{
          display: 'flex',
          padding: '12px 20px',
          border: '1px solid #333',
          borderRadius: 10,
          background: 'rgba(10,10,10,0.85)',
          color: '#e5e5e5',
          fontSize: 20,
        }}
      >
        <span style={{ color: '#737373', marginRight: 14 }}>$</span>
        curl -fsSL aka.visvesh.dev/install.sh | sh
      </div>
    ),
  });
}
