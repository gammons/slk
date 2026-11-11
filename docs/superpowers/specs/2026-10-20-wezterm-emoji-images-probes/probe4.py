#!/usr/bin/env python3
"""Fourth WezTerm probe: kitty delete semantics and re-upload over live placements."""
import base64
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from probe import IMG_A, IMG_B, apc, cup, disc, label, place, png, w  # noqa: E402

IMG_C = base64.b64encode(png(64, 64, disc((230, 40, 40)))).decode()  # red

PURPLE = "\x1b[48;2;80;0;80m"
RST = "\x1b[0m"


def slots(row, text, ids_cols):
    label(row, text)
    for iid, col, p in ids_cols:
        place(row, col, iid, extra=f",p={p}")


def main():
    w("\x1b[?1049h\x1b[2J\x1b[H")
    label(1, "agent-test probe4")
    for iid in (41, 42, 43, 44):
        apc(f"a=t,f=100,t=d,i={iid},q=2;{IMG_A}")
    apc(f"a=t,f=100,t=d,i=45,q=2;{IMG_B}")
    time.sleep(0.3)

    # R1: delete one placement by (i, p); bg must survive.
    label(3, f"R1 d=i,p=2 middle only: {PURPLE}[  ]{RST} {PURPLE}[  ]{RST} {PURPLE}[  ]{RST}  expect Y _ Y, purple stays")
    for p, col in ((1, 26), (2, 31), (3, 36)):
        place(3, col, 41, extra=f",p={p}")
    time.sleep(0.4)
    apc("a=d,d=i,i=41,p=2,q=2")

    # R2: delete all placements + data (d=I).
    slots(5, "R2 d=I all:              [  ] [  ]  expect both empty", [(42, 27, 1), (42, 32, 2)])
    time.sleep(0.4)
    apc("a=d,d=I,i=42,q=2")

    # R3: d=i without p -> all placements gone, data kept; then re-place p=3.
    slots(7, "R3 d=i no p, then p=3:   [  ] [  ] [  ]  expect _ _ Y", [(43, 27, 1), (43, 32, 2)])
    time.sleep(0.4)
    apc("a=d,d=i,i=43,q=2")
    time.sleep(0.2)
    place(7, 37, 43, extra=",p=3")

    # R4: re-upload SAME data over live placements.
    slots(9, "R4 re-upload same data:  [  ] [  ]  survive?", [(44, 27, 1), (44, 32, 2)])
    time.sleep(0.4)
    apc(f"a=t,f=100,t=d,i=44,q=2;{IMG_A}")

    # R5: re-upload DIFFERENT data over a live placement.
    slots(11, "R5 re-upload new data:   [  ]  blue->red? gone? stays blue?", [(45, 27, 1)])
    time.sleep(0.4)
    apc(f"a=t,f=100,t=d,i=45,q=2;{IMG_C}")

    # R6: slk's real upload prefix: a=d,d=I then a=t, over live placements.
    apc(f"a=t,f=100,t=d,i=46,q=2;{IMG_B}")
    time.sleep(0.2)
    slots(13, "R6 a=d,d=I + a=t re-up:   [  ]  (slk upload prefix)", [(46, 27, 1)])
    time.sleep(0.4)
    apc("a=d,d=I,i=46,q=2")
    apc(f"a=t,f=100,t=d,i=46,q=2;{IMG_B}")

    cup(15, 1)
    w("done; sleeping")
    time.sleep(float(sys.argv[1]) if len(sys.argv) > 1 else 20)
    w("\x1b[?1049l")


if __name__ == "__main__":
    main()
