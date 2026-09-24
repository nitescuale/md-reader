#!/usr/bin/env python3
"""Generate assets/icon.ico for MdReader (no external dependencies).

Draws the classic markdown "rounded square + M + down arrow" mark at high
resolution with 2x2 supersampling, then area-resamples it into the standard
icon sizes and writes a multi-image .ico (BMP entries, PNG for 128/256).
"""
import math
import os
import struct
import sys
import zlib

SS = 2          # supersampling factor
R = 512         # render resolution
BG = (31, 111, 235)   # #1f6feb
FG = (255, 255, 255)

# rounded rect
RECT = (24.0, 24.0, 488.0, 488.0)
RADIUS = 92.0

# strokes: (x0, y0, x1, y1) polylines, pen half-width
PEN = 19.0
M_PATH = [(108.0, 372.0), (108.0, 168.0), (179.0, 300.0), (250.0, 168.0), (250.0, 372.0)]
ARROW_STEM = (350.0, 152.0, 350.0, 336.0)
ARROW_HEAD = ((306.0, 296.0), (394.0, 296.0), (350.0, 386.0))


def seg_dist(px, py, x0, y0, x1, y1):
    dx, dy = x1 - x0, y1 - y0
    l2 = dx * dx + dy * dy
    if l2 == 0:
        return math.hypot(px - x0, py - y0)
    t = ((px - x0) * dx + (py - y0) * dy) / l2
    t = 0.0 if t < 0 else (1.0 if t > 1 else t)
    return math.hypot(px - (x0 + t * dx), py - (y0 + t * dy))


def in_round_rect(px, py):
    x0, y0, x1, y1 = RECT
    if px < x0 or px > x1 or py < y0 or py > y1:
        return False
    # corners
    if px < x0 + RADIUS and py < y0 + RADIUS:
        return math.hypot(px - (x0 + RADIUS), py - (y0 + RADIUS)) <= RADIUS
    if px > x1 - RADIUS and py < y0 + RADIUS:
        return math.hypot(px - (x1 - RADIUS), py - (y0 + RADIUS)) <= RADIUS
    if px < x0 + RADIUS and py > y1 - RADIUS:
        return math.hypot(px - (x0 + RADIUS), py - (y1 - RADIUS)) <= RADIUS
    if px > x1 - RADIUS and py > y1 - RADIUS:
        return math.hypot(px - (x1 - RADIUS), py - (y1 - RADIUS)) <= RADIUS
    return True


def in_triangle(px, py, a, b, c):
    def sign(p1, p2, p3):
        return (p1[0] - p3[0]) * (p2[1] - p3[1]) - (p2[0] - p3[0]) * (p1[1] - p3[1])
    d1 = sign((px, py), a, b)
    d2 = sign((px, py), b, c)
    d3 = sign((px, py), c, a)
    neg = (d1 < 0) or (d2 < 0) or (d3 < 0)
    pos = (d1 > 0) or (d2 > 0) or (d3 > 0)
    return not (neg and pos)


def glyph(px, py):
    for i in range(len(M_PATH) - 1):
        x0, y0 = M_PATH[i]
        x1, y1 = M_PATH[i + 1]
        if seg_dist(px, py, x0, y0, x1, y1) <= PEN:
            return True
    if seg_dist(px, py, *ARROW_STEM) <= PEN:
        return True
    if in_triangle(px, py, *ARROW_HEAD):
        return True
    return False


def render():
    h = R * SS
    step = 1.0 / SS
    rows = []
    for y in range(R):
        row = bytearray()
        for x in range(R):
            bg_cov = 0
            fg_cov = 0
            for sy in range(SS):
                for sx in range(SS):
                    px = x + (sx + 0.5) * step
                    py = y + (sy + 0.5) * step
                    if in_round_rect(px, py):
                        bg_cov += 1
                        if glyph(px, py):
                            fg_cov += 1
            n = SS * SS
            a = bg_cov / n
            f = fg_cov / n if bg_cov else 0.0
            r = BG[0] * (1 - f) + FG[0] * f
            g = BG[1] * (1 - f) + FG[1] * f
            b = BG[2] * (1 - f) + FG[2] * f
            row += bytes((int(b + 0.5), int(g + 0.5), int(r + 0.5), int(a * 255 + 0.5)))
        rows.append(row)
    return rows


def resize(src, size):
    n = len(src)
    scale = n / size
    out = []
    for ty in range(size):
        y0, y1 = ty * scale, (ty + 1) * scale
        iy0, iy1 = int(y0), int(math.ceil(y1))
        row = bytearray()
        for tx in range(size):
            x0, x1 = tx * scale, (tx + 1) * scale
            ix0, ix1 = int(x0), int(math.ceil(x1))
            acc = [0.0, 0.0, 0.0, 0.0]
            wsum = 0.0
            for sy in range(iy0, min(iy1, n)):
                wy = min(sy + 1, y1) - max(sy, y0)
                if wy <= 0:
                    continue
                srow = src[sy]
                for sx in range(ix0, min(ix1, n)):
                    wx = min(sx + 1, x1) - max(sx, x0)
                    if wx <= 0:
                        continue
                    w = wx * wy
                    o = sx * 4
                    acc[0] += srow[o] * w
                    acc[1] += srow[o + 1] * w
                    acc[2] += srow[o + 2] * w
                    acc[3] += srow[o + 3] * w
                    wsum += w
            if wsum <= 0:
                row += b"\x00\x00\x00\x00"
            else:
                row += bytes(min(255, int(c / wsum + 0.5)) for c in acc)
        out.append(row)
    return out


def png_bytes(rows, size):
    # ICO BMP entries are BGRA; PNG wants RGBA, so swap the red/blue channels.
    rgb = [bytes(b for px in range(size) for b in (row[px * 4 + 2], row[px * 4 + 1],
                                                   row[px * 4], row[px * 4 + 3]))
           for row in rows]
    raw = b"".join(b"\x00" + bytes(r) for r in rgb)
    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr)
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def bmp_bytes(rows, size):
    # BITMAPINFOHEADER with doubled height, BGRA bottom-up, plus a zeroed AND mask
    hdr = struct.pack("<IiiHHIIiiII", 40, size, size * 2, 1, 32, 0, size * size * 4, 0, 0, 0, 0)
    px = b"".join(bytes(rows[y]) for y in range(size - 1, -1, -1))
    mask_row = ((size + 31) // 32) * 4
    mask = b"\x00" * (mask_row * size)
    return hdr + px + mask


def main():
    out_path = sys.argv[1] if len(sys.argv) > 1 else "assets/icon.ico"
    os.makedirs(os.path.dirname(out_path), exist_ok=True)
    big = render()
    sizes = [16, 32, 48, 64, 128, 256]
    images = []
    for s in sizes:
        rows = resize(big, s)
        data = png_bytes(rows, s) if s >= 128 else bmp_bytes(rows, s)
        images.append((s, data))
        print("size %3d -> %6d bytes" % (s, len(data)))
    out = struct.pack("<HHH", 0, 1, len(images))
    offset = 6 + 16 * len(images)
    entries = b""
    for s, data in images:
        dim = 0 if s == 256 else s
        entries += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(data), offset)
        offset += len(data)
    with open(out_path, "wb") as f:
        f.write(out + entries + b"".join(d for _, d in images))
    print("wrote %s (%d bytes)" % (out_path, os.path.getsize(out_path)))


if __name__ == "__main__":
    main()
