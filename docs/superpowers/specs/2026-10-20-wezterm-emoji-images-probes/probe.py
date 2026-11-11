#!/usr/bin/env python3
"""Visual probe of WezTerm image behaviour for slk's emoji-as-images.

Run inside a WezTerm window. Draws labelled test rows, then waits so the
window can be screenshotted. Every test places a synthetic 64x64 RGBA
"emoji" (a colored disc with a transparent corner) into a 2x1 cell slot.
"""
import base64
import os
import struct
import sys
import time
import zlib


def png(w, h, rgba_fn):
    raw = bytearray()
    for y in range(h):
        raw.append(0)
        for x in range(w):
            raw.extend(rgba_fn(x, y))
    def chunk(t, d):
        c = struct.pack(">I", len(d)) + t + d
        return c + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(bytes(raw))) + chunk(b"IEND", b""))


def disc(color):
    def f(x, y):
        dx, dy = x - 31.5, y - 31.5
        if dx * dx + dy * dy <= 30 * 30:
            return (*color, 255)
        return (0, 0, 0, 0)
    return f


IMG_A = base64.b64encode(png(64, 64, disc((255, 200, 0)))).decode()   # yellow
IMG_B = base64.b64encode(png(64, 64, disc((0, 160, 255)))).decode()   # blue

out = sys.stdout


def w(s):
    out.write(s)
    out.flush()


def apc(body):
    w("\x1b_G" + body + "\x1b\\")


def cup(row, col):
    w(f"\x1b[{row};{col}H")


def label(row, text):
    cup(row, 1)
    w(f"\x1b[2K{text}")


def place(row, col, img_id, extra=""):
    w("\x1b7")
    cup(row, col)
    apc(f"a=p,i={img_id},c=2,r=1,C=1,q=2{extra}")
    w("\x1b8")


DIAC = ["̅", "̍", "̎", "̐"]


def main():
    w("\x1b[?1049h\x1b[2J\x1b[H")  # alt screen, clear
    # Upload two images, transmit only (no display).
    apc(f"a=t,f=100,t=d,i=7,q=2;{IMG_A}")
    apc(f"a=t,f=100,t=d,i=8,q=2;{IMG_B}")

    label(1, f"agent-test wezprobe  TERM_PROGRAM={os.environ.get('TERM_PROGRAM')} ver={os.environ.get('TERM_PROGRAM_VERSION')}")

    # T1: direct placement a=p,c=2,r=1,C=1 inside a text row.
    label(3, "T1 a=p c2 r1 C=1: [  ] <- yellow disc expected in the brackets, text after stays: AFTER")
    place(3, 26, 7)

    # T2: same image placed several times on one row + another image.
    label(5, "T2 multi:  [  ] [  ] [  ] [  ]  (Y Y B Y)")
    for col, iid in ((13, 7), (18, 7), (23, 8), (28, 7)):
        place(5, col, iid)

    # T3: place, then overwrite the cells with text -> image should go.
    label(7, "T3 overwrite by text: [  ] should show 'XX' and NO disc")
    place(7, 24, 7)
    w("\x1b7"); cup(7, 24); w("XX"); w("\x1b8")

    # T4: place, then ECH the cells -> image should go.
    label(9, "T4 ECH erase: [  ] should be empty, NO disc")
    place(9, 16, 7)
    w("\x1b7"); cup(9, 16); w("\x1b[2X"); w("\x1b8")

    # T5: kitty unicode placeholders (U=1 virtual placement).
    label(11, "T5 U=1 placeholders: [")
    apc(f"a=T,f=100,t=d,i=9,U=1,c=2,r=1,q=2;{IMG_B}")
    w("\x1b[38;2;0;0;9m" + "\U0010EEEE" + DIAC[0] + DIAC[0] + "\U0010EEEE" + DIAC[0] + DIAC[1] + "\x1b[39m] blue disc = placeholders supported")

    # T6: iTerm2 OSC 1337 inline image, width=2 height=1 (cells).
    label(13, "T6 OSC1337 w2 h1: [")
    w("\x1b7")
    w(f"\x1b]1337;File=inline=1;width=2;height=1;preserveAspectRatio=1;doNotMoveCursor=1:{IMG_A}\x07")
    w("\x1b8")
    cup(13, 22)
    w("] text after (cursor restored with DECRC)")

    # T7: background colour behind a transparent image.
    label(15, "T7 bg: \x1b[48;2;80;0;80m[  ]\x1b[0m  purple bg must show around the disc")
    place(15, 9, 8)

    # T8: scroll region -> do images move with their text rows?
    label(17, "T8 scroll: row A [  ]  (disc must move up with this text)")
    place(17, 18, 7)
    label(18, "T8 scroll: row B")
    label(19, "T8 scroll: row C (this row scrolls away)")
    # Scroll rows 17..19 region by deleting row 17's predecessor: set region 16-19, scroll up 1.
    w("\x1b[16;19r")  # DECSTBM
    w("\x1b[1S")      # SU 1: row17 -> row16
    w("\x1b[r")       # reset region
    label(19, "T8 after SU: row A should now be on the line above 'row B'")

    # T9: a=p with x,y,w,h source crop: left half only.
    label(21, "T9 crop left half (w=32): [  ] half disc")
    place(21, 29, 7, extra=",x=0,y=0,w=32,h=64")

    cup(23, 1)
    w("done; sleeping")
    time.sleep(float(sys.argv[1]) if len(sys.argv) > 1 else 20)
    w("\x1b[?1049l")


if __name__ == "__main__":
    main()
