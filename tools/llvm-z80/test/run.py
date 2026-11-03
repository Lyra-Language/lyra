#!/usr/bin/env python3
"""Compile IR with the Z80 target and run it on Sheliak's Z80 core.

    tools/llvm-z80/test/run.py [FILE.ll ...]    # default: every test here

Each `; test NAME REG=VALUE` in a file is held to the host compiler's answer for the same IR,
then run at -O2 and at -O0: a ROM that sets the stack, calls NAME and halts. Linking here is only for these tests: sections in a row from address
0x100, `__z80_callbc` as its two bytes, each relocation applied.
"""
import os, re, struct, subprocess, sys, tempfile

HERE = os.path.dirname(os.path.abspath(__file__))
LLVM = os.environ.get("LYRA_Z80_LLVM", os.path.expanduser("~/Dev/llvm-z80"))
LLC = os.path.join(LLVM, "build/bin/llc")
RUN = os.environ.get("Z80RUN", os.path.join(HERE, "../../../../sheliak/build/z80run"))
RUNTIME = os.path.join(HERE, "../../../runtime/sms/runtime.ll")
ORIGIN = 0x100
EM_Z80 = 220


def read_elf(path):
    d = open(path, "rb").read()
    assert d[:4] == b"\x7fELF" and d[4] == 1 and d[5] == 1, path
    machine = struct.unpack_from("<H", d, 18)[0]
    assert machine == EM_Z80, f"{path}: machine {machine}"
    shoff = struct.unpack_from("<I", d, 32)[0]
    shentsize, shnum, shstrndx = struct.unpack_from("<HHH", d, 46)
    secs = []
    for i in range(shnum):
        secs.append(struct.unpack_from("<10I", d, shoff + i * shentsize))
    def name_at(tab, off):
        base = secs[tab][4] + off
        return d[base:d.index(b"\0", base)].decode()
    return d, secs, name_at, shstrndx


def link(objects, start):
    image = bytearray(ORIGIN)
    placed, symbols, pending = {}, {}, []
    pc = ORIGIN
    image += bytes([0xC5, 0xC9])
    symbols["__z80_callbc"] = pc
    pc += 2
    # pkg/rom/z80.go's z80MultiplyCode.
    multiply = bytes([0x44, 0x4D, 0x21, 0, 0, 0x3E, 0x10, 0x14, 0x15, 0x20, 0x05, 0x53, 0x1E, 0, 0x3E, 0x08, 0x29, 0xCB, 0x13, 0xCB, 0x12, 0x30, 0x04,
                      0x09, 0x30, 0x01, 0x13, 0x3D, 0x20, 0xF2, 0xC9])
    image += multiply
    symbols["__mulhi3"] = symbols["__z80_umul32"] = pc
    pc += len(multiply)
    # And its z80DivideCode, with the call's address filled in here.
    divide = bytes([0x44, 0x4D, 0x21, 0, 0, 0x3E, 0x10, 0xCB, 0x21, 0xCB, 0x10, 0xED, 0x6A, 0x38, 0x08,
                    0xB7, 0xED, 0x52, 0x30, 0x06, 0x19, 0x18, 0x04, 0xB7, 0xED, 0x52, 0x0C, 0x3D, 0x20,
                    0xE9, 0xC9, 0xCD, pc & 255, pc >> 8, 0x60, 0x69, 0xC9])
    image += divide
    symbols["__umodhi3"] = pc
    symbols["__udivhi3"] = pc + 31
    pc += len(divide)
    for index, path in enumerate(objects):
        d, secs, name_at, strndx = read_elf(path)
        for i, s in enumerate(secs):
            name, kind, flags, size, off = name_at(strndx, s[0]), s[1], s[2], s[5], s[4]
            if not flags & 2 or kind not in (1, 8):
                continue
            placed[(index, i)] = pc
            image += d[off:off + size] if kind == 1 else bytes(size)
            pc += size
        for i, s in enumerate(secs):
            if s[1] == 2:
                strtab = s[6]
                for k in range(s[5] // 16):
                    nm, val, _, info, _, shndx = struct.unpack_from("<IIIBBH", d, s[4] + k * 16)
                    name = name_at(strtab, nm)
                    if name and shndx and (info >> 4) >= 1 and shndx < 0xff00:
                        symbols[name] = placed[(index, shndx)] + val
        for i, s in enumerate(secs):
            if s[1] == 4 and (index, s[7]) in placed:
                symsec = secs[s[6]]
                for k in range(s[5] // 12):
                    off, info, addend = struct.unpack_from("<IIi", d, s[4] + k * 12)
                    nm, val, _, sinfo, _, shndx = struct.unpack_from(
                        "<IIIBBH", d, symsec[4] + (info >> 8) * 16)
                    pending.append((placed[(index, s[7])] + off, info & 255, addend,
                                    name_at(symsec[6], nm), (index, shndx), val))
    for where, kind, addend, name, sec, val in pending:
        if name and name not in symbols:
            raise SystemExit(f"nothing defines {name}")
        value = (symbols[name] if name else placed[sec] + val) + addend
        if kind == 4:
            struct.pack_into("<H", image, where, value & 0xFFFF)
        elif kind == 1:
            image[where] = value & 0xFF
        elif kind == 6:
            struct.pack_into("<I", image, where, value & 0xFFFFFFFF)
        else:
            raise SystemExit(f"relocation {kind}")
    target = symbols[start]
    # ld sp, 0xFF00; call target; halt
    image[0:7] = bytes([0x31, 0x00, 0xFF, 0xCD, target & 255, target >> 8, 0x76])
    assert pc < 0xF000, "image too large"
    return bytes(image)


def host_values(path, tmp):
    """Each test's value as the host's compiler has it: the same IR, built natively."""
    source = open(path).read()
    tests = [line.split()[2:] for line in source.splitlines() if line.startswith("; test ")]
    calls, text = [], source
    for i, (name, want) in enumerate(tests):
        reg = want.split("=")[0]
        ret = re.search(r"^define (\w+) @" + name + r"\(", source, re.M).group(1)
        calls.append(f"  %v{i} = call {ret} @{name}()")
        value = f"%v{i}"
        if reg == "de":
            calls.append(f"  %s{i} = lshr {ret} %v{i}, 16")
            value = f"%s{i}"
        calls.append(f"  %t{i} = zext {ret} {value} to i64" if ret in ("i8", "i16")
                     else f"  %t{i} = and {ret} {value}, 65535")
        wide = f"%t{i}" if ret in ("i8", "i16") else f"%w{i}"
        if ret == "i32":
            calls.append(f"  %w{i} = zext i32 %t{i} to i64")
        elif ret == "i64":
            calls.append(f"  %w{i} = add i64 %t{i}, 0")
        calls.append(f'  call i32 (ptr, ...) @printf(ptr @fmt, ptr @n{i}, i64 {wide})')
        text += f'@n{i} = constant [{len(name) + 1} x i8] c"{name}\\00"\n'
    text += '@fmt = constant [10 x i8] c"%s %llu\\0A\\00\\00"\ndeclare i32 @printf(ptr, ...)\n'
    text += "define i32 @main() {\n" + "\n".join(calls) + "\n  ret i32 0\n}\n"
    ll = os.path.join(tmp, "host.ll")
    exe = os.path.join(tmp, "host")
    open(ll, "w").write(text)
    subprocess.check_call(["clang", "-w", "-x", "ir", ll, "-o", exe])
    out = subprocess.check_output([exe], text=True)
    return dict(line.split() for line in out.splitlines())


def main():
    files = sys.argv[1:] or [os.path.join(HERE, f) for f in sorted(os.listdir(HERE))
                             if f.endswith(".ll")]
    failures = total = 0
    with tempfile.TemporaryDirectory() as tmp:
        for path in files:
            if open(path).readline().startswith("; no-host"):
                continue
            host = host_values(path, tmp)
            for line in open(path):
                if line.startswith("; test "):
                    _, _, name, want = line.split()
                    if host[name] != str(int(want.split("=")[1], 0)):
                        failures += 1
                        print(f"WRONG TEST {name}: says {want}, the host computes {host[name]}")
        for level in ("-O2", "-O0"):
            rt = os.path.join(tmp, f"rt{level}.o")
            subprocess.check_call([LLC, "-mtriple=z80-unknown-elf", level, "-filetype=obj",
                                   RUNTIME, "-o", rt])
            for path in files:
                obj = os.path.join(tmp, f"t{level}.o")
                subprocess.check_call([LLC, "-mtriple=z80-unknown-elf", level, "-filetype=obj",
                                       path, "-o", obj])
                for line in open(path):
                    if not line.startswith("; test "):
                        continue
                    _, _, name, want = line.split()
                    reg, value = want.split("=")
                    rom = os.path.join(tmp, "t.bin")
                    open(rom, "wb").write(link([obj, rt], name))
                    out = subprocess.run([RUN, rom], capture_output=True, text=True).stdout
                    got = dict(p.split("=") for p in out.split())
                    total += 1
                    if got.get(reg) != str(int(value, 0)) or got.get("halted") != "1":
                        failures += 1
                        print(f"FAIL {level} {name}: want {want}, got {out.strip()}")
    print(f"{total - failures}/{total} passed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
