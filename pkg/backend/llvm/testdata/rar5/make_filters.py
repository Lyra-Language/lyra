# Hand-built RAR5 archives exercising each filter (usage: python3 make_filters.py DIR): one compressed block, every byte a
# literal (9-bit codes), preceded by one filter command. No RAR tool is needed to make them,
# and libarchive (bsdtar) is the independent decoder they are checked against.
import sys, zlib, random, struct

class Bits:
    def __init__(s): s.out = bytearray(); s.n = 0
    def put(s, value, count):          # MSB first, as RAR5 reads
        for i in range(count - 1, -1, -1):
            if s.n % 8 == 0: s.out.append(0)
            s.out[-1] |= ((value >> i) & 1) << (7 - s.n % 8)
            s.n += 1

def vint(v):
    out = bytearray()
    while True:
        b = v & 0x7F; v >>= 7
        out.append(b | (0x80 if v else 0))
        if not v: return bytes(out)

def header(kind, flags, fields, extra=b'', data_size=None):
    body = vint(kind) + vint(flags | (1 if extra else 0) | (2 if data_size is not None else 0))
    if extra: body += vint(len(extra))
    if data_size is not None: body += vint(data_size)
    body += fields + extra
    sized = vint(len(body)) + body
    return struct.pack('<I', zlib.crc32(sized)) + sized

def block(window, filter_kind, start, length, channels=0):
    b = Bits()
    for _ in range(20): b.put(5, 4)                       # bit-length code: all length 5
    lengths = [9] * 256 + [2] + [0] * 49 + [0] * 64 + [0] * 16 + [0] * 44
    for l in lengths: b.put(l, 5)                         # symbol l has code l (5 bits)
    b.put(0, 2)                                           # symbol 256 (code 00): a filter
    def number(v):
        n = 1 if v < 0x100 else 2 if v < 0x10000 else 3
        b.put(n - 1, 2)
        for i in range(n): b.put((v >> (8 * i)) & 0xFF, 8)
    number(start); number(length); b.put(filter_kind, 3)
    if filter_kind == 0: b.put(channels - 1, 5)
    for byte in window: b.put(128 + byte, 9)              # literal k: code 128 + k
    data = bytes(b.out)
    used = b.n % 8 or 8
    size_bytes = 1 if len(data) < 0x100 else 2 if len(data) < 0x10000 else 3
    flags = 0x80 | 0x40 | ((size_bytes - 1) << 3) | (used - 1)
    size = len(data).to_bytes(size_bytes, 'little')
    check = 0x5A ^ flags
    for x in size: check ^= x
    return bytes([flags, check]) + size + data

def archive(name, window, filter_kind, start, length, channels=0):
    packed = block(window, filter_kind, start, length, channels)
    main = header(1, 0, vint(0))
    fname = name.encode()
    fields = vint(0) + vint(len(window)) + vint(0x20) + vint(5 << 7) + vint(0) + vint(len(fname)) + fname
    file_header = header(2, 0, fields, data_size=len(packed))
    end = header(5, 0, vint(0))
    return b'Rar!\x1a\x07\x01\x00' + main + file_header + packed + end

random.seed(3)
def x86ish(n):
    w = bytearray(random.getrandbits(8) for _ in range(n))
    for i in range(0, n - 5, 7):                           # calls and jumps, targets of both signs
        w[i] = random.choice([0xE8, 0xE9])
        w[i + 1:i + 5] = struct.pack('<i', random.choice([-1, 1]) * random.randrange(0, 0x2000000))
    return bytes(w)
def armish(n):
    w = bytearray(random.getrandbits(8) for _ in range(n))
    for i in range(0, n - 3, 8): w[i + 3] = 0xEB
    return bytes(w)
out = sys.argv[1]
cases = [
    ('e8', x86ish(3000), 1, 0, 3000, 0),
    ('e8e9', x86ish(3000), 2, 0, 3000, 0),
    ('e8-offset', x86ish(3000), 2, 700, 2000, 0),
    ('arm', armish(2000), 3, 0, 2000, 0),
    ('delta', bytes(random.getrandbits(8) for _ in range(1500)), 0, 100, 1200, 3),
]
for name, window, kind, start, length, channels in cases:
    open(f'{out}/filter-{name}.rar', 'wb').write(archive(name + '.bin', window, kind, start, length, channels))
print(len(cases), 'archives')
