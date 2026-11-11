#!/usr/bin/env python3
"""Fifth WezTerm probe: do placements WITH p= survive text / ECH / EL over their cells?"""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from probe import IMG_A, IMG_B, apc, cup, label, place, w  # noqa: E402


def main():
    w("\x1b[?1049h\x1b[2J\x1b[H")
    label(1, "agent-test probe5 (all placements below use p=)")
    apc(f"a=t,f=100,t=d,i=51,q=2;{IMG_A}")
    apc(f"a=t,f=100,t=d,i=52,q=2;{IMG_B}")
    time.sleep(0.3)

    label(3, "S1 p=1, then text 'XX':   [  ]  removed?")
    place(3, 28, 51, extra=",p=1")
    time.sleep(0.3)
    w("\x1b7"); cup(3, 28); w("XX"); w("\x1b8")

    label(5, "S2 p=2, then ECH 2:       [  ]  removed?")
    place(5, 28, 51, extra=",p=2")
    time.sleep(0.3)
    w("\x1b7"); cup(5, 28); w("\x1b[2X"); w("\x1b8")

    label(7, "S3 p=3, then EL2+redraw:  [  ]  removed?")
    place(7, 28, 51, extra=",p=3")
    time.sleep(0.3)
    label(7, "S3 p=3, then EL2+redraw:  [  ]  removed?")

    label(9, "S4 p=4, then EL0 from col 20: [  ]  removed?")
    place(9, 32, 51, extra=",p=4")
    time.sleep(0.3)
    w("\x1b7"); cup(9, 20); w("\x1b[0K"); w("nD, then EL0 from col 20: [  ]  removed?"); w("\x1b8")

    label(11, "S5 p=5, then same text rewritten in place (no EL): [  ]")
    place(11, 52, 52, extra=",p=5")
    time.sleep(0.3)
    w("\x1b7"); cup(11, 1); w("S5 p=5, then same text rewritten in place (no EL): [  ]"); w("\x1b8")

    label(13, "S6 p=6 then a=d,d=i,i=52,p=6: [  ]  removed")
    place(13, 32, 52, extra=",p=6")
    time.sleep(0.3)
    apc("a=d,d=i,i=52,p=6,q=2")

    cup(15, 1)
    w("done; sleeping")
    time.sleep(float(sys.argv[1]) if len(sys.argv) > 1 else 20)
    w("\x1b[?1049l")


if __name__ == "__main__":
    main()
