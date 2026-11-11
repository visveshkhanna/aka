#!/usr/bin/env python3
"""Generates the site's icons and Open Graph background from the source
artwork in assets/brand/. Run it after changing that artwork, and commit
the results; the site build does not need Python.

    python3 scripts/brand-assets.py
"""
from pathlib import Path

from PIL import Image

root = Path(__file__).resolve().parent.parent
src = root / "assets" / "brand"
icon = Image.open(src / "icon.png").convert("RGBA")
art = Image.open(src / "og-background.png").convert("RGB")


def save(img, path, **kw):
    path.parent.mkdir(parents=True, exist_ok=True)
    img.save(path, **kw)
    print(f"{path.relative_to(root)}  {img.size[0]}x{img.size[1]}  {path.stat().st_size // 1024} KB")


def sized(img, n):
    return img.resize((n, n), Image.LANCZOS)


# Favicons: Next.js serves app/favicon.ico and app/icon.png and links them.
save(sized(icon, 256), root / "app" / "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)])
save(sized(icon, 512), root / "app" / "icon.png", optimize=True)

# Apple touch icon: opaque and square; iOS rounds the corners itself.
apple = Image.new("RGBA", icon.size, (0, 0, 0, 255))
apple.alpha_composite(icon)
save(sized(apple, 180).convert("RGB"), root / "app" / "apple-icon.png", optimize=True)

# Web app manifest icons and the logo in the navigation bar.
for n in (192, 512):
    save(sized(icon, n), root / "public" / "brand" / f"icon-{n}.png", optimize=True)
save(sized(icon, 64), root / "public" / "brand" / "logo-64.png", optimize=True)

# Open Graph background: 1200x630 (1.91:1), cropped evenly from the source.
w, h = art.size
target_h = round(w / 1.905)
top = (h - target_h) // 2
og = art.crop((0, top, w, top + target_h)).resize((1200, 630), Image.LANCZOS)
save(og, root / "assets" / "og-background.jpg", quality=90, optimize=True, progressive=True)
