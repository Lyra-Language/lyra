#!/usr/bin/env python3
"""Generate the sprite sheet `examples/SDL3/nes/sprites.lyra` and `tilemap.lyra` draw.

The PNG is committed, so this only needs running when the art changes — it is here so the
image is reproducible and reviewable rather than an opaque binary that arrived somehow.

    python3 examples/SDL3/nes/assets/generate.py

**NES rules**: every sprite is 16×16, uses at most three colours plus transparency, and
takes those colours from the 2C02 palette — read from `../palette.lyra`, so there is one
copy of it. Hard edges only: the program samples with nearest-neighbour, so anti-aliasing
would only show up as smudges.

Cells 9–12 are background tiles for `tilemap.lyra`: grass-topped ground, dirt, a cloud and
a bush; 13–14 the goal's pole and flag-topped pole for `game.lyra`. Always appended, so
every earlier index is unchanged.

The slime is drawn in greys on purpose. SDL's colour mod multiplies, so the program tints
one grey drawing into several palettes, the way NES games reused a sprite with a
different palette.

Pure standard library, like `examples/raylib/assets/generate.py`.
"""

import os
import re
import struct
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
CELL = 16


def nes_palette():
    """The 64 packed RGB values from palette.lyra's PALETTE table."""
    with open(os.path.join(HERE, "..", "palette.lyra")) as f:
        source = f.read()
    table = source[source.index("const PALETTE"):]
    table = table[table.index("[", table.index("=")) + 1:table.index("]", table.index("="))]
    values = [int(v, 16) for v in re.findall(r"0x([0-9A-Fa-f]{6})", table)]
    assert len(values) == 64, len(values)
    return values


PALETTE = nes_palette()


def nes(index):
    v = PALETTE[index]
    return ((v >> 16) & 255, (v >> 8) & 255, v & 255, 255)


CLEAR = (0, 0, 0, 0)


def write_png(path, width, height, pixels):
    """pixels: a list of (r, g, b, a) tuples, row-major from the top left."""
    raw = b"".join(
        b"\x00" + b"".join(bytes(pixels[y * width + x]) for x in range(width))
        for y in range(height)
    )

    def chunk(kind, data):
        body = kind + data
        return struct.pack(">I", len(data)) + body + struct.pack(">I", zlib.crc32(body) & 0xFFFFFFFF)

    png = b"\x89PNG\r\n\x1a\n"
    png += chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress(raw, 9))
    png += chunk(b"IEND", b"")
    with open(path, "wb") as f:
        f.write(png)


def from_art(rows, colours):
    """A cell from 16 strings of 16 characters; '.' is transparent."""
    assert len(rows) == CELL, len(rows)
    cell = []
    for row in rows:
        assert len(row) == CELL, row
        cell.extend(CLEAR if ch == "." else nes(colours[ch]) for ch in row)
    return cell


# The explorer: helmet (B), skin (S), boots and eyes (K).
EXPLORER = {"B": 0x12, "S": 0x37, "K": 0x07}
EXPLORER_TOP = [
    "................",
    ".....BBBBBB.....",
    "....BBBBBBBBB...",
    "...BBBBBBBBBBB..",
    "....SSSSKSSK....",
    "....SSSSKSSK....",
    "....SSSSSSSS....",
    ".....SSSSSS.....",
    "....BBBBBBBB....",
    "...BBBBBBBBBB...",
    "..SSBBBBBBBBSS..",
    "..SSBBBBBBBBSS..",
]
EXPLORER_STRIDE = [
    "....BBB..BBB....",
    "...BBB....BBB...",
    "..KKKK....KKKK..",
    "..KKK......KKK..",
]
EXPLORER_TOGETHER = [
    ".....BBBBBB.....",
    ".....BBB.BB.....",
    ".....KKK.KK.....",
    "....KKKK.KKK....",
]


def coin(half_width):
    """A coin seen edge-on to face-on: gold (0x28), rim (0x17), glint (0x30)."""
    cell = []
    cx, cy, half_height = 7.5, 7.5, 6.5
    for y in range(CELL):
        for x in range(CELL):
            dx = (x - cx) / max(half_width, 0.5)
            dy = (y - cy) / half_height
            d = dx * dx + dy * dy
            if d > 1.0:
                cell.append(CLEAR)
            elif d > 0.55 or half_width < 1.5:
                cell.append(nes(0x17))
            elif x < cx - half_width / 3 and abs(y - cy) < 3:
                cell.append(nes(0x30))
            else:
                cell.append(nes(0x28))
    return cell


def brick():
    """A two-course brick block: face (0x17), top highlight (0x27), mortar (0x0F)."""
    cell = []
    for y in range(CELL):
        for x in range(CELL):
            course = y // 8
            joint = 0 if course == 0 else 8
            if y % 8 == 7 or x == (joint + 15) % 16:
                cell.append(nes(0x0F))
            elif y % 8 == 0:
                cell.append(nes(0x27))
            else:
                cell.append(nes(0x17))
    return cell


def slime(half_width, height):
    """A dome sitting on the cell's floor: body white (0x30), shade (0x10), outline (0x00)."""
    cell = []
    cx, floor = 7.5, 15.5
    eye_row = int(floor - height + height * 0.45)
    for y in range(CELL):
        for x in range(CELL):
            dx = (x - cx) / half_width
            dy = (floor - y) / height
            d = dx * dx + dy * dy
            if y > 15 or d > 1.0:
                cell.append(CLEAR)
            elif d > 0.7 or y == 15:
                cell.append(nes(0x00))
            elif y == eye_row and x in (5, 10):
                cell.append(nes(0x00))
            elif d > 0.45 or x > cx + 2:
                cell.append(nes(0x10))
            else:
                cell.append(nes(0x30))
    return cell


def ground(grass):
    """Ground: dirt (0x17) with a fixed speckle (0x07), under a grass top (0x2A/0x1A)."""
    cell = []
    for y in range(CELL):
        for x in range(CELL):
            if grass and y < 2:
                cell.append(nes(0x2A))
            elif grass and y < 4:
                cell.append(nes(0x1A) if (x + y) % 3 else nes(0x2A))
            elif (x * 7 + y * 13) % 17 == 0:
                cell.append(nes(0x07))
            else:
                cell.append(nes(0x17))
    return cell


def blobs(circles, fill, shade, outline):
    """A shape made of overlapping circles: outline at the rim, shade on the lower half."""
    cell = []
    for y in range(CELL):
        for x in range(CELL):
            d = min(((x - cx) ** 2 + (y - cy) ** 2) / (r * r) for cx, cy, r in circles)
            if d > 1.0:
                cell.append(CLEAR)
            elif d > 0.72:
                cell.append(nes(outline))
            elif y > 10:
                cell.append(nes(shade))
            else:
                cell.append(nes(fill))
    return cell


def flag_width(y):
    """The pennant's width on row y: seven pixels at its middle row (9), tapering to a
    point at rows 5 and 13."""
    return 7 - abs(y - 9) * 7 // 4


def pole(top):
    """The goal: a white pole (0x30, shaded 0x10). The top cell adds a green ball (0x2A)
    and the flag (0x2A, edged 0x1A) flying to the left."""
    cell = []
    for y in range(CELL):
        for x in range(CELL):
            if top and (x - 7.5) ** 2 + (y - 2.5) ** 2 <= 5.5:
                cell.append(nes(0x2A))
            elif x in (7, 8) and (not top or y >= 4):
                cell.append(nes(0x30) if x == 7 else nes(0x10))
            elif top and 5 <= y <= 13 and 7 - flag_width(y) <= x < 7:
                edge = x == 7 - flag_width(y)
                cell.append(nes(0x1A) if edge else nes(0x2A))
            else:
                cell.append(CLEAR)
    return cell


CELLS = [
    from_art(EXPLORER_TOP + EXPLORER_STRIDE, EXPLORER),
    from_art(EXPLORER_TOP + EXPLORER_TOGETHER, EXPLORER),
    coin(6.5),
    coin(4.0),
    coin(1.0),
    coin(4.0),
    brick(),
    slime(7.5, 11.0),
    slime(8.0, 8.0),
    # Background tiles, appended so the sprite indices above keep their places.
    ground(grass=True),
    ground(grass=False),
    blobs([(4.5, 10.0, 4.5), (8.5, 7.0, 5.5), (12.0, 10.5, 4.0)], 0x30, 0x31, 0x21),
    blobs([(4.0, 14.0, 5.5), (8.5, 11.5, 6.5), (12.5, 14.0, 5.0)], 0x2A, 0x1A, 0x0A),
    # The goal, for `game.lyra`.
    pole(top=False),
    pole(top=True),
]


def main():
    width, height = CELL * len(CELLS), CELL
    pixels = [CLEAR] * (width * height)
    for i, cell in enumerate(CELLS):
        for y in range(CELL):
            for x in range(CELL):
                pixels[y * width + i * CELL + x] = cell[y * CELL + x]
    path = os.path.join(HERE, "sprites.png")
    write_png(path, width, height, pixels)
    print(f"wrote {path} ({width}x{height}, {len(CELLS)} cells)")


if __name__ == "__main__":
    main()
