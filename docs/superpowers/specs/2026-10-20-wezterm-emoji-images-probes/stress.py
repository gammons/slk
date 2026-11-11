#!/usr/bin/env python3
"""Throughput probe: ~N 2x1 kitty placements repainted every frame, like a scroll.

Each frame rewrites every text row (shifted content, as a scroll would) inside
synchronized output, then re-places every emoji slot with a slot-unique p.
Writes timing to the file given as argv[1].
"""
import base64
import os
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from probe import disc, png  # noqa: E402

out = os.fdopen(sys.stdout.fileno(), "wb", buffering=0)

COLORS = [(255, 200, 0), (0, 160, 255), (230, 40, 40), (40, 200, 90), (200, 80, 220),
          (255, 128, 0), (0, 200, 200), (128, 128, 128), (250, 250, 250), (120, 60, 20)]
IMGS = [base64.b64encode(png(64, 64, disc(c))).decode() for c in COLORS]


def main():
    log_path = sys.argv[1]
    fps = float(sys.argv[2]) if len(sys.argv) > 2 else 30
    seconds = float(sys.argv[3]) if len(sys.argv) > 3 else 5
    cols, rows = os.get_terminal_size()
    buf = bytearray(b"\x1b[?1049h\x1b[2J\x1b[H\x1b[?25l")
    for i, img in enumerate(IMGS):
        buf += f"\x1b_Ga=t,f=100,t=d,i={100 + i},q=2;{img}\x1b\\".encode()
    out.write(bytes(buf))
    time.sleep(0.5)

    per_row = max(1, (cols - 4) // 8)  # one emoji every 8 columns
    frames = int(fps * seconds)
    t0 = time.monotonic()
    total_bytes = 0
    slots_per_frame = 0
    for f in range(frames):
        frame = bytearray(b"\x1b[?2026h")
        slots_per_frame = 0
        for r in range(1, rows):
            line_no = r + f
            text = "".join(f"m{line_no % 100:02d}   " + "  " for _ in range(per_row))[: cols - 1]
            frame += f"\x1b[{r};1H\x1b[2K{text}".encode()
        for r in range(1, rows):
            for k in range(per_row):
                col = 1 + k * 8 + 5
                iid = 100 + (r + f + k) % len(IMGS)
                p = r * 1000 + col
                frame += f"\x1b7\x1b[{r};{col}H\x1b_Ga=p,i={iid},p={p},c=2,r=1,C=1,q=2\x1b\\\x1b8".encode()
                slots_per_frame += 1
        frame += f"\x1b[{rows};1H\x1b[2Kframe {f + 1}/{frames} slots={slots_per_frame}".encode()
        frame += b"\x1b[?2026l"
        out.write(bytes(frame))
        total_bytes += len(frame)
        target = t0 + (f + 1) / fps
        now = time.monotonic()
        if target > now:
            time.sleep(target - now)
    elapsed = time.monotonic() - t0
    with open(log_path, "w") as fh:
        fh.write(f"term={cols}x{rows} slots/frame={slots_per_frame} frames={frames} fps_target={fps}\n")
        fh.write(f"elapsed={elapsed:.2f}s (ideal {seconds:.2f}s) bytes/frame={total_bytes // frames}\n")
    time.sleep(4)
    out.write(b"\x1b[?25h\x1b[?1049l")


if __name__ == "__main__":
    main()
