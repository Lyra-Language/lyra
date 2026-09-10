#!/usr/bin/env python3
"""Generate the PNGs `examples/raylib/textures.lyra` draws.

The images are committed, so this only needs running when they change — it is here so
they are reproducible and reviewable rather than opaque binaries that arrived somehow.

    python3 examples/raylib/assets/generate.py

Pure standard library: PNG is a zlib stream in a handful of length-prefixed chunks, which
is little enough code to be worth writing rather than taking a dependency for three files.
Everything is rasterised at 4x and box-filtered down, which is what gives the edges their
anti-aliasing — raylib's bilinear filter has nothing to show on hard-edged pixel art.
"""

import math
import os
import struct
import zlib

SS = 4  # supersampling factor


def write_png(path, width, height, pixels):
    """pixels: a list of (r, g, b, a) tuples, row-major from the top left."""
    rows = []
    for y in range(height):
        row = bytearray()
        row.append(0)  # filter type 0 (None)
        for x in range(width):
            row.extend(pixels[y * width + x])
        rows.append(bytes(row))
    raw = b"".join(rows)

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    png = b"\x89PNG\r\n\x1a\n"
    png += chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress(raw, 9))
    png += chunk(b"IEND", b"")
    with open(path, "wb") as f:
        f.write(png)
    print(f"  {os.path.basename(path):14s} {width}x{height}  {len(png):>6d} bytes")


class Canvas:
    """A supersampled RGBA canvas with the few primitives these images need."""

    def __init__(self, width, height):
        self.w, self.h = width * SS, height * SS
        self.out_w, self.out_h = width, height
        self.px = [(0, 0, 0, 0)] * (self.w * self.h)

    def _blend(self, i, color):
        sr, sg, sb, sa = color
        if sa == 0:
            return
        if sa == 255:
            self.px[i] = color
            return
        dr, dg, db, da = self.px[i]
        a = sa / 255.0
        self.px[i] = (
            int(sr * a + dr * (1 - a)),
            int(sg * a + dg * (1 - a)),
            int(sb * a + db * (1 - a)),
            max(sa, da),
        )

    def fill(self, color):
        self.px = [color] * (self.w * self.h)

    def rect(self, x, y, w, h, color):
        x0, y0 = int(x * SS), int(y * SS)
        x1, y1 = int((x + w) * SS), int((y + h) * SS)
        for py in range(max(0, y0), min(self.h, y1)):
            for px in range(max(0, x0), min(self.w, x1)):
                self._blend(py * self.w + px, color)

    def disc(self, cx, cy, r, color):
        cx, cy, r = cx * SS, cy * SS, r * SS
        for py in range(max(0, int(cy - r)), min(self.h, int(cy + r) + 1)):
            for px in range(max(0, int(cx - r)), min(self.w, int(cx + r) + 1)):
                if (px - cx) ** 2 + (py - cy) ** 2 <= r * r:
                    self._blend(py * self.w + px, color)

    def rounded_rect(self, x, y, w, h, radius, color):
        self.rect(x + radius, y, w - 2 * radius, h, color)
        self.rect(x, y + radius, w, h - 2 * radius, color)
        for cx, cy in ((x + radius, y + radius), (x + w - radius, y + radius),
                       (x + radius, y + h - radius), (x + w - radius, y + h - radius)):
            self.disc(cx, cy, radius, color)

    def vertical_gradient(self, x, y, w, h, top, bottom):
        for row in range(int(h * SS)):
            t = row / max(1, h * SS - 1)
            color = tuple(int(top[i] + (bottom[i] - top[i]) * t) for i in range(4))
            self.rect(x, y + row / SS, w, 1 / SS, color)

    def downsample(self):
        out = []
        n = SS * SS
        for y in range(self.out_h):
            for x in range(self.out_w):
                r = g = b = a = 0
                for dy in range(SS):
                    for dx in range(SS):
                        pr, pg, pb, pa = self.px[(y * SS + dy) * self.w + x * SS + dx]
                        # Weight colour by coverage so transparent pixels do not drag
                        # the edges toward black — the classic halo.
                        r += pr * pa
                        g += pg * pa
                        b += pb * pa
                        a += pa
                if a == 0:
                    out.append((0, 0, 0, 0))
                else:
                    out.append((r // a, g // a, b // a, a // n))
        return out


def sprite_sheet():
    """Four 32x32 frames of a creature bobbing along: the sheet the example animates."""
    frames, size = 4, 32
    c = Canvas(size * frames, size)
    body = (86, 140, 220, 255)
    shade = (58, 104, 176, 255)
    foot = (44, 62, 96, 255)
    eye_white = (250, 250, 255, 255)
    pupil = (24, 28, 40, 255)

    for f in range(frames):
        ox = f * size
        bob = (0, -1, 0, 1)[f]
        cy = 17 + bob
        # legs, swinging out of phase
        swing = (-3, 0, 3, 0)[f]
        c.rounded_rect(ox + 11 + swing, cy + 6, 4, 7, 2, foot)
        c.rounded_rect(ox + 17 - swing, cy + 6, 4, 7, 2, foot)
        # body, with a darker underside
        c.disc(ox + 16, cy, 9, shade)
        c.disc(ox + 16, cy - 1, 8.5, body)
        # a highlight, so scaling it up shows something other than a flat disc
        c.disc(ox + 13, cy - 4, 2.6, (140, 186, 246, 255))
        # eye
        c.disc(ox + 19, cy - 2, 3.2, eye_white)
        c.disc(ox + 20 + (0, 1, 0, -1)[f] * 0.6, cy - 2, 1.5, pupil)
    return size * frames, size, c.downsample()


def panel():
    """A 48x48 nine-patch: a rounded, outlined frame whose corners must not stretch."""
    n = 48
    c = Canvas(n, n)
    c.rounded_rect(0, 0, n, n, 12, (108, 78, 168, 255))          # outer frame
    c.rounded_rect(2, 2, n - 4, n - 4, 10, (150, 118, 210, 255))  # inner bevel
    c.vertical_gradient(6, 6, n - 12, n - 12, (246, 243, 255, 255), (214, 205, 240, 255))
    return n, n, c.downsample()


def scene():
    """A small landscape, for the plain draw and tint demos.

    Something with real gradients and overlapping shapes, so a tint is visibly a tint and
    a magnified copy visibly shows the filter.
    """
    w, h = 160, 100
    c = Canvas(w, h)
    c.vertical_gradient(0, 0, w, 62, (126, 192, 238, 255), (206, 232, 246, 255))
    c.disc(126, 22, 11, (255, 232, 150, 255))
    c.disc(126, 22, 7.5, (255, 248, 208, 255))
    # hills, back to front
    c.disc(40, 78, 40, (128, 172, 116, 255))
    c.disc(104, 82, 44, (104, 150, 96, 255))
    c.rect(0, 62, w, h - 62, (96, 142, 90, 255))
    c.rect(0, 74, w, h - 74, (78, 122, 74, 255))
    # a path
    for i in range(28):
        t = i / 27
        c.disc(70 + 26 * math.sin(t * 2.4), 74 + t * 26, 3 + t * 5, (198, 176, 132, 255))
    return w, h, c.downsample()


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    print("writing:")
    for name, make in (("sprites.png", sprite_sheet), ("panel.png", panel), ("scene.png", scene)):
        width, height, pixels = make()
        write_png(os.path.join(here, name), width, height, pixels)


if __name__ == "__main__":
    main()
