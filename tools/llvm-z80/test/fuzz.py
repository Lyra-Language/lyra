#!/usr/bin/env python3
"""Random programs, the Z80's answers held to the host compiler's.

    tools/llvm-z80/test/fuzz.py [COUNT] [SEED]

Each program is a function of straight-line i8/i16/i32 arithmetic and comparisons, with
branches and a stack array, whose inputs arrive through calls so nothing folds away. It
returns a checksum of everything computed, at -O2 and -O0.
"""
import os, random, subprocess, sys, tempfile

sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import run as harness

WIDTH = {"i8": 8, "i16": 16, "i32": 32}
BIN = ["add", "sub", "and", "or", "xor"]
CMP = ["eq", "ne", "ult", "ule", "ugt", "uge", "slt", "sle", "sgt", "sge"]


def program(rng):
    lines, values, n = [], {"i8": [], "i16": [], "i32": []}, 0
    def fresh():
        nonlocal n
        n += 1
        return f"%v{n}"
    def pick(t):
        return rng.choice(values[t])
    for t, fn in (("i8", "b"), ("i16", "w"), ("i32", "d")):
        for _ in range(3):
            v = fresh()
            k = rng.choice([0, 1, 2, (1 << WIDTH[t]) - 1, 1 << (WIDTH[t] - 1),
                            rng.randrange(1 << WIDTH[t])])
            lines.append(f"  {v} = call {t} @{fn}({t} {k})")
            values[t].append(v)
    lines.append("  %arr = alloca [6 x i16]")
    for i in range(6):
        lines.append(f"  %slot{i} = getelementptr [6 x i16], ptr %arr, i16 0, i16 {i}")
        lines.append(f"  store i16 {pick('i16')}, ptr %slot{i}")
    block = 0
    for _ in range(rng.randrange(15, 45)):
        t = rng.choice(["i8", "i16", "i32"])
        v = fresh()
        op = rng.randrange(10)
        if op < 4:
            rhs = pick(t) if rng.random() < .7 else rng.randrange(1 << WIDTH[t])
            lines.append(f"  {v} = {rng.choice(BIN)} {t} {pick(t)}, {rhs}")
        elif op == 4 and t != "i32":
            lines.append(f"  {v} = mul {t} {pick(t)}, {pick(t)}")
        elif op == 5:
            amount = rng.randrange(WIDTH[t])
            lines.append(f"  {v} = {rng.choice(['shl', 'lshr', 'ashr'])} {t} {pick(t)}, {amount}")
        elif op == 6:
            c = fresh()
            lines.append(f"  {c} = icmp {rng.choice(CMP)} {t} {pick(t)}, {pick(t)}")
            lines.append(f"  {v} = select i1 {c}, {t} {pick(t)}, {t} {pick(t)}")
        elif op == 7:
            s = rng.choice([x for x in WIDTH if WIDTH[x] < WIDTH[t]] or ["i32"])
            if s == "i32" or WIDTH[s] >= WIDTH[t]:
                lines.append(f"  {v} = trunc i32 {pick('i32')} to {'i16' if t == 'i32' else t}")
                t = "i16" if t == "i32" else t
            else:
                lines.append(f"  {v} = {rng.choice(['zext', 'sext'])} {s} {pick(s)} to {t}")
        elif op == 8:
            t = "i16"
            mask = fresh()
            i = fresh()
            p = fresh()
            lines.append(f"  {mask} = urem i16 {pick('i16')}, 6")
            lines.append(f"  {p} = getelementptr [6 x i16], ptr %arr, i16 0, i16 {mask}")
            if rng.random() < .5:
                lines.append(f"  store i16 {pick('i16')}, ptr {p}")
            lines.append(f"  {v} = load i16, ptr {p}")
            del i
        else:
            c = fresh()
            block += 1
            lines.append(f"  {c} = icmp {rng.choice(CMP)} {t} {pick(t)}, {pick(t)}")
            a, b = pick(t), pick(t)
            lines.append(f"  br i1 {c}, label %then{block}, label %else{block}")
            lines.append(f"then{block}:")
            x = fresh()
            lines.append(f"  {x} = add {t} {a}, 1")
            lines.append(f"  br label %join{block}")
            lines.append(f"else{block}:")
            lines.append(f"  br label %join{block}")
            lines.append(f"join{block}:")
            lines.append(f"  {v} = phi {t} [ {x}, %then{block} ], [ {b}, %else{block} ]")
        values[t].append(v)
    # Everything into one 16-bit checksum.
    total = "0"
    for t, vs in values.items():
        for v in vs:
            w = v
            if t != "i16":
                w = fresh()
                lines.append(f"  {w} = {'zext' if t == 'i8' else 'trunc'} {t} {v} to i16")
            r = fresh()
            s = fresh()
            lines.append(f"  {r} = shl i16 {total}, 1")
            lines.append(f"  {s} = xor i16 {r}, {w}")
            total = s
    return ("define i8 @b(i8 %x) { ret i8 %x }\ndefine i16 @w(i16 %x) { ret i16 %x }\n"
            "define i32 @d(i32 %x) { ret i32 %x }\n"
            "define i16 @fuzz() {\n" + "\n".join(lines) + f"\n  ret i16 {total}\n}}\n")


def main():
    count = int(sys.argv[1]) if len(sys.argv) > 1 else 200
    seed = int(sys.argv[2]) if len(sys.argv) > 2 else 1
    failures = 0
    with tempfile.TemporaryDirectory() as tmp:
        for level in ("-O2", "-O0"):
            subprocess.check_call([harness.LLC, "-mtriple=z80-unknown-elf", level, "-filetype=obj",
                                   os.path.join(harness.HERE, "rt.ll"), "-o",
                                   os.path.join(tmp, f"rt{level}.o")])
        for i in range(count):
            text = program(random.Random(seed + i))
            ll = os.path.join(tmp, "f.ll")
            open(ll, "w").write("; test fuzz hl=0\n" + text)
            want = harness.host_values(ll, tmp)["fuzz"]
            for level in ("-O2", "-O0"):
                obj = os.path.join(tmp, "f.o")
                build = subprocess.run([harness.LLC, "-mtriple=z80-unknown-elf", level, "-filetype=obj",
                                        ll, "-o", obj], capture_output=True, text=True)
                if build.returncode:
                    failures += 1
                    print(f"seed {seed + i} {level}: llc: {build.stderr.splitlines()[0][:150]}")
                    continue
                rom = os.path.join(tmp, "f.bin")
                open(rom, "wb").write(harness.link([obj, os.path.join(tmp, f"rt{level}.o")], "fuzz"))
                out = subprocess.run([harness.RUN, rom], capture_output=True, text=True).stdout
                got = dict(p.split("=") for p in out.split())
                if got.get("hl") != want or got.get("halted") != "1":
                    failures += 1
                    print(f"seed {seed + i} {level}: want hl={want}, got {out.strip()}")
    print(f"{count * 2 - failures}/{count * 2} agreed with the host")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
