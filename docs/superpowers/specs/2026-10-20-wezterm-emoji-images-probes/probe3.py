#!/usr/bin/env python3
"""Third WezTerm probe: placement-id semantics, overwrite/ECH removal, OSC 1337 repeats."""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from probe import IMG_A, IMG_B, apc, cup, label, place, w  # noqa: E402


def iterm(row, col, data):
    w("\x1b7")
    cup(row, col)
    w(f"\x1b]1337;File=inline=1;width=2;height=1;preserveAspectRatio=1;doNotMoveCursor=1:{data}\x07")
    w("\x1b8")


def main():
    w("\x1b[?1049h\x1b[2J\x1b[H")
    label(1, "agent-test probe3")
    for iid, img in ((31, IMG_A), (32, IMG_B), (34, IMG_A), (35, IMG_B), (36, IMG_A)):
        apc(f"a=t,f=100,t=d,i={iid},q=2;{img}")
    time.sleep(0.3)

    label(3, "Q1 id31 p=1,2,3 only:          [  ] [  ] [  ]")
    for n, col in enumerate((33, 38, 43), start=1):
        place(3, col, 31, extra=f",p={n}")

    label(5, "Q2 id32 no-p then p=5:         [  ] [  ]")
    place(5, 33, 32)
    place(5, 38, 32, extra=",p=5")

    label(7, "Q3 id34 placed, wait, text XX: [  ]  (expect XX, no disc)")
    place(7, 33, 34)
    time.sleep(0.4)
    w("\x1b7"); cup(7, 33); w("XX"); w("\x1b8")

    label(9, "Q4 id35 placed, wait, ECH 2:   [  ]  (expect empty)")
    place(9, 33, 35)
    time.sleep(0.4)
    w("\x1b7"); cup(9, 33); w("\x1b[2X"); w("\x1b8")

    label(11, "Q5 id36 placed, wait, EL2 row, same text redrawn: [  ] (expect empty)")
    place(11, 52, 36)
    time.sleep(0.4)
    label(11, "Q5 id36 placed, wait, EL2 row, same text redrawn: [  ] (expect empty)")

    label(13, "Q6 OSC1337 x3 one row:         [  ] [  ] [  ]")
    for col in (33, 38, 43):
        iterm(13, col, IMG_A)

    label(15, "Q7 OSC1337 x3, then XX / ECH:  [  ] [  ] [  ]  (expect XX, empty, disc)")
    for col in (33, 38, 43):
        iterm(15, col, IMG_B)
    time.sleep(0.4)
    w("\x1b7"); cup(15, 33); w("XX"); cup(15, 38); w("\x1b[2X"); w("\x1b8")

    label(17, "Q8 OSC1337 then same slot again 5x: [  ]")
    for _ in range(5):
        iterm(17, 38, IMG_A)

    cup(19, 1)
    w("done; sleeping")
    time.sleep(float(sys.argv[1]) if len(sys.argv) > 1 else 20)
    w("\x1b[?1049l")


if __name__ == "__main__":
    main()
