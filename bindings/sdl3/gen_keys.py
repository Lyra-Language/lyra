#!/usr/bin/env python3
"""Generate bindings/sdl3/keys.lyra from the installed SDL3 headers.

    python3 bindings/sdl3/gen_keys.py

Every `SDL_Scancode`, every `SDLK_` key code and every `SDL_KMOD_` modifier, transcribed
by a program rather than by hand: there are about five hundred, and a hand-copied table
is wrong in exactly the places nobody looks. The output is committed, so building Lyra
needs no SDL headers; rerun this when SDL is upgraded and review the diff.

Names follow the bindings' convention — the SDL prefix becomes the Lyra one:

    SDL_SCANCODE_PAGEUP -> SCANCODE_PAGEUP   (physical position, u32)
    SDLK_PAGEUP         -> KEY_PAGEUP        (layout-dependent key code, u32)
    SDL_KMOD_CTRL       -> MOD_CTRL          (modifier mask, u16)

The table is checked by properties rather than by a second transcription —
`examples/SDL3/nes/editor.lyra --check` asks SDL itself to name the codes.

Standard library only. Headers are found with `pkg-config --variable=includedir sdl3`.
"""

import os
import re
import subprocess

HERE = os.path.dirname(os.path.abspath(__file__))


def sdl(what):
    return subprocess.run(
        ["pkg-config", what, "sdl3"], capture_output=True, text=True, check=True
    ).stdout.strip()


INCLUDE = os.path.join(sdl("--variable=includedir"), "SDL3")
VERSION = sdl("--modversion")


def read(name):
    with open(os.path.join(INCLUDE, name)) as f:
        return f.read()


def scancodes():
    """(name, value) for each `SDL_SCANCODE_X = N,` enum entry, in header order."""
    source = read("SDL_scancode.h")
    body = source[source.index("typedef enum SDL_Scancode"):source.index("} SDL_Scancode;")]
    out = []
    for name, value in re.findall(r"^\s*SDL_SCANCODE_(\w+)\s*=\s*(\d+)", body, re.M):
        if name in ("RESERVED", "COUNT"):
            continue  # markers, not keys
        out.append((name, int(value)))
    return out


def keycodes():
    """(name, value) for each `#define SDLK_X 0x...u`, skipping the two masks."""
    source = read("SDL_keycode.h")
    out = []
    for name, value in re.findall(r"^#define SDLK_(\w+)\s+0x([0-9a-fA-F]+)u", source, re.M):
        out.append((name, int(value, 16)))
    return out


def modifiers():
    """(name, value) for each `SDL_KMOD_`, resolving the ORed combinations."""
    source = read("SDL_keycode.h")
    values = {}
    for name, expr in re.findall(r"^#define SDL_KMOD_(\w+)\s+(.+?)\s*(?:/\*|$)", source, re.M):
        expr = expr.strip()
        if expr.startswith("("):
            parts = re.findall(r"SDL_KMOD_(\w+)", expr)
            values[name] = 0
            for p in parts:
                values[name] |= values[p]
        else:
            values[name] = int(expr.rstrip("u"), 16)
    return list(values.items())


def main():
    lines = [
        "module bindings.sdl3",
        f"//! Every SDL {VERSION} scancode, key code and key modifier — **generated** by",
        "//! `gen_keys.py` from the SDL headers; do not edit, rerun it.",
        "//!",
        "//! - `SCANCODE_*` is a key's **position** (`SDL_Scancode`): `SCANCODE_W` is the key",
        "//!   where W is on a US keyboard, whatever it is labelled. What a game's controls want;",
        "//!   read with `key_held`.",
        "//! - `KEY_*` is the **key code** the layout produces (`SDLK_*`): `KEY_A` is whichever",
        "//!   key types an `a`. What shortcuts and text want; arrives in `KeyDown`/`KeyUp`.",
        "//!   Printable keys are their lowercase ASCII code; the rest are their scancode with",
        "//!   bit 30 set (`SDLK_SCANCODE_MASK`).",
        "//! - `MOD_*` is a modifier mask (`SDL_Keymod`), for `modifiers()`. `MOD_CTRL` and",
        "//!   friends cover either side; `MOD_GUI` is Command on a Mac.",
        "",
    ]
    lines.append("// Scancodes")
    for name, value in scancodes():
        lines.append(f"pub const SCANCODE_{name}: u32 = {value}")
    lines.append("")
    lines.append("// Key codes")
    for name, value in keycodes():
        lines.append(f"pub const KEY_{name}: u32 = 0x{value:08x}")
    lines.append("")
    lines.append("/// Every scancode above, for a program (or a check) that walks them all.")
    lines.append("pub const SCANCODES: []u32 = [")
    for name, _ in scancodes():
        lines.append(f"  SCANCODE_{name},")
    lines.append("]")
    lines.append("")
    lines.append("/// Every key code above.")
    lines.append("pub const KEY_CODES: []u32 = [")
    for name, _ in keycodes():
        lines.append(f"  KEY_{name},")
    lines.append("]")
    lines.append("")
    lines.append("// Modifiers")
    for name, value in modifiers():
        lines.append(f"pub const MOD_{name}: u16 = 0x{value:04x}")
    path = os.path.join(HERE, "keys.lyra")
    with open(path, "w") as f:
        f.write("\n".join(lines) + "\n")
    print(f"wrote {path}: {len(scancodes())} scancodes, {len(keycodes())} key codes, "
          f"{len(modifiers())} modifiers (SDL {VERSION})")


if __name__ == "__main__":
    main()
