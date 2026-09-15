#!/usr/bin/env python3
"""
EdKeyBridge 程序图标生成器（纯标准库，无 PIL / ImageMagick 依赖）。

设计：深蓝圆角徽章 + 暖色键帽(带键柄)，读作“键盘按键”。
同一份图元列表同时驱动预览 PNG 与多尺寸 .ico，保证成品与预览一致。
图标可再生：改 design() 后重跑即可。

输出：
  icon/app.ico       多尺寸 .ico（16/24/32/48/64 用 DIB，128/256 内嵌 PNG）
  icon/preview.png   256px 预览图（给人看）
"""
import struct
import zlib
import math
import os

W = 256  # 设计坐标系尺寸

# 调色板
BG = (38, 50, 86)      # 深蓝徽章
KEY = (255, 176, 87)   # 暖色键帽
RIM = (255, 255, 255)  # 极淡白描边（深色任务栏上留边界）


def clamp01(v):
    return 0.0 if v < 0 else (1.0 if v > 1 else v)


def sd_round_box(px, py, cx, cy, hw, hh, r):
    """有符号距离函数：圆角矩形。正=外部，负=内部。"""
    qx = abs(px - cx) - (hw - r)
    qy = abs(py - cy) - (hh - r)
    ax = max(qx, 0.0)
    ay = max(qy, 0.0)
    return math.hypot(ax, ay) + min(max(qx, qy), 0.0) - r


def design():
    """返回图层列表（自底向上）：(sdf函数, 颜色(RGB), 填充透明度)。"""
    return [
        (lambda px, py: sd_round_box(px, py, 128, 128, 110, 110, 44), RIM, 0.14),
        (lambda px, py: sd_round_box(px, py, 128, 128, 104, 104, 40), BG, 1.0),
        # 键柄(上)与键帽(下)重叠，合成一个连贯的“按键”轮廓
        (lambda px, py: sd_round_box(px, py, 128, 96, 22, 48, 10), KEY, 1.0),
        (lambda px, py: sd_round_box(px, py, 128, 154, 50, 44, 18), KEY, 1.0),
    ]


def render(size):
    aa = W / size
    img = bytearray(size * size * 4)
    for y in range(size):
        for x in range(size):
            px = (x + 0.5) * aa
            py = (y + 0.5) * aa
            cr = cg = cb = ca = 0.0
            for sdf, col, fa in design():
                d = sdf(px, py)
                cov = clamp01(0.5 - d / aa) * fa
                if cov <= 0:
                    continue
                na = cov + ca * (1 - cov)
                if na <= 0:
                    continue
                cr = (col[0] * cov + cr * ca * (1 - cov)) / na
                cg = (col[1] * cov + cg * ca * (1 - cov)) / na
                cb = (col[2] * cov + cb * ca * (1 - cov)) / na
                ca = na
            i = (y * size + x) * 4
            img[i] = int(cr + 0.5)
            img[i + 1] = int(cg + 0.5)
            img[i + 2] = int(cb + 0.5)
            img[i + 3] = int(ca * 255 + 0.5)
    return img


def dib_bytes(size, img):
    """小尺寸用 32bpp DIB（XOR 自下而上 + 1bpp AND 掩码全 0）。"""
    xor = bytearray()
    for y in range(size - 1, -1, -1):
        xor += img[y * size * 4:(y + 1) * size * 4]
    row_bytes = ((size + 31) // 32) * 4
    and_mask = b"\x00" * (row_bytes * size)
    header = struct.pack("<IiiHHIIiiII", 40, size, size * 2, 1, 32, 0, 0, 0, 0, 0, 0)
    return header + xor + and_mask


def png_bytes(size, img):
    def chunk(typ, data):
        c = typ + data
        return struct.pack(">I", len(data)) + c + struct.pack(">I", zlib.crc32(c) & 0xFFFFFFFF)

    ihdr = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    raw = bytearray()
    for y in range(size):
        raw.append(0)
        raw += img[y * size * 4:(y + 1) * size * 4]
    idat = zlib.compress(bytes(raw), 9)
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", ihdr) + chunk(b"IDAT", idat) + chunk(b"IEND", b"")


def ico_bytes(entries):
    """entries: [(size, data_bytes), ...]"""
    n = len(entries)
    icondir = struct.pack("<HHH", 0, 1, n)
    offset = 6 + n * 16
    dirs = b""
    datas = b""
    for size, data in entries:
        w = 0 if size >= 256 else size
        dirs += struct.pack("<BBBBHHII", w, w, 0, 0, 1, 32, len(data), offset + len(datas))
        datas += data
    return icondir + dirs + datas


def main():
    out_dir = os.path.join(os.path.dirname(__file__), "..", "icon")
    out_dir = os.path.abspath(out_dir)
    os.makedirs(out_dir, exist_ok=True)

    sizes = [16, 24, 32, 48, 64, 128, 256]
    entries = []
    for s in sizes:
        img = render(s)
        if s >= 128:
            entries.append((s, png_bytes(s, img)))
        else:
            entries.append((s, dib_bytes(s, img)))
        if s == 256:
            with open(os.path.join(out_dir, "preview.png"), "wb") as f:
                f.write(png_bytes(s, img))

    with open(os.path.join(out_dir, "app.ico"), "wb") as f:
        f.write(ico_bytes(entries))
    print("生成完成:", os.path.join(out_dir, "app.ico"), "尺寸:", sizes)


if __name__ == "__main__":
    main()
