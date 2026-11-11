#!/usr/bin/env python3
"""Second WezTerm probe: isolate why the first a=p placements were missing."""
import sys
import time

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from probe import IMG_A, IMG_B, apc, cup, label, place, w  # noqa: E402


def main():
    w("\x1b[?1049h\x1b[2J\x1b[H")
    label(1, "agent-test probe2")

    # P1: upload a=t, place IMMEDIATELY (same write burst).
    apc(f"a=t,f=100,t=d,i=21,q=2;{IMG_A}")
    label(3, "P1 upload+place same burst:   [  ]")
    place(3, 32, 21)

    # P2: upload a=t, sleep, then place.
    apc(f"a=t,f=100,t=d,i=22,q=2;{IMG_B}")
    time.sleep(0.5)
    label(5, "P2 upload, sleep 0.5s, place: [  ]")
    place(5, 32, 22)

    # P3: same image placed 3x on one row, after a sleep (ids already decoded).
    time.sleep(0.3)
    label(7, "P3 same id 3x one row:        [  ] [  ] [  ]")
    for col in (32, 37, 42):
        place(7, col, 22)

    # P4: same id, explicit distinct placement ids.
    label(9, "P4 same id, p=1,2,3:          [  ] [  ] [  ]")
    for n, col in enumerate((32, 37, 42), start=1):
        place(9, col, 21, extra=f",p={n}")

    # P5: visible placement then overwrite by text after a pause.
    label(11, "P5 placed then text 'XX':     [  ]")
    place(11, 32, 22)
    time.sleep(0.5)
    w("\x1b7"); cup(11, 32); w("XX"); w("\x1b8")

    # P6: visible placement then ECH after a pause.
    label(13, "P6 placed then ECH 2:         [  ]  (expect empty)")
    place(13, 32, 22)
    time.sleep(0.5)
    w("\x1b7"); cup(13, 32); w("\x1b[2X"); w("\x1b8")

    # P7: re-place the same slot repeatedly (painter repaint) -> no stacking artefacts.
    label(15, "P7 same slot placed 5x:       [  ]")
    for _ in range(5):
        place(15, 32, 21)

    # P8: overwrite the row with identical spaces (bubbletea rewrite) then repaint.
    label(17, "P8 row rewritten, repainted:  [  ]")
    place(17, 32, 22)
    time.sleep(0.3)
    label(17, "P8 row rewritten, repainted:  [  ]")
    place(17, 32, 22)

    # P9: second upload of a NEW id while others visible; immediately place twice.
    apc(f"a=t,f=100,t=d,i=23,q=2;{IMG_A}")
    label(19, "P9 new id, immediate x2:      [  ] [  ]")
    place(19, 32, 23)
    place(19, 37, 23)

    cup(21, 1)
    w("done; sleeping")
    time.sleep(float(sys.argv[1]) if len(sys.argv) > 1 else 20)
    w("\x1b[?1049l")


if __name__ == "__main__":
    main()
