#!/usr/bin/env bash
# Drags across a skill's markdown and checks the highlight spans more than one line.
#
# Uses the in-process `drag` command (window points, no OS cursor). The selection
# color is the Synca palette's selected ink, #0d3a36. A pass needs that color on
# two text rows with a gap of ordinary pixels between them.
#
# Needs `ns-dev` on PATH (or NS_DEV) and a Synca Dev app from `scripts/install-dev.sh synca`
# in ns-gpui. Do not run `scope name=workspace` before this check: it clears the open
# project. NS_SELECT_INDEX picks another sidebar row (default 1).
set -euo pipefail

nsdev="${NS_DEV:-ns-dev}"
out="${1:-/tmp/synca-markdown-select.png}"
index="${NS_SELECT_INDEX:-1}"

"$nsdev" synca launch >/dev/null
"$nsdev" synca resize 1280 860 >/dev/null
"$nsdev" synca select "index=$index" >/dev/null
"$nsdev" synca scroll end >/dev/null
sleep 0.4
"$nsdev" synca drag x=600 y=620 to_x=900 to_y=780 >/dev/null
sleep 0.4
"$nsdev" synca shot "$out" >/dev/null

python3 - "$out" <<'PY'
import struct
import sys
import zlib
from pathlib import Path

path = Path(sys.argv[1])
data = path.read_bytes()
assert data[:8] == bytes([137, 80, 78, 71, 13, 10, 26, 10]), "not a png"

pos = 8
width = height = None
idat = []
while pos < len(data):
    length = struct.unpack(">I", data[pos : pos + 4])[0]
    kind = data[pos + 4 : pos + 8]
    body = data[pos + 8 : pos + 8 + length]
    if kind == b"IHDR":
        width, height, depth, color = struct.unpack(">IIBB", body[:10])
        assert depth == 8 and color in (2, 6), f"expected 8-bit rgb or rgba, got depth {depth} color {color}"
        channels = 4 if color == 6 else 3
    elif kind == b"IDAT":
        idat.append(body)
    elif kind == b"IEND":
        break
    pos += 12 + length

raw = zlib.decompress(b"".join(idat))
stride = width * channels + 1
rows = []
prev = bytearray(width * channels)
off = 0
for _ in range(height):
    filt = raw[off]
    row = bytearray(raw[off + 1 : off + 1 + width * channels])
    if filt == 1:
        for i in range(channels, len(row)):
            row[i] = (row[i] + row[i - channels]) & 255
    elif filt == 2:
        for i in range(len(row)):
            row[i] = (row[i] + prev[i]) & 255
    elif filt == 3:
        for i in range(len(row)):
            left = row[i - channels] if i >= channels else 0
            row[i] = (row[i] + ((left + prev[i]) // 2)) & 255
    elif filt == 4:
        for i in range(len(row)):
            a = row[i - channels] if i >= channels else 0
            b = prev[i]
            c = prev[i - channels] if i >= channels else 0
            guess = a + b - c
            pa, pb, pc = abs(guess - a), abs(guess - b), abs(guess - c)
            pick = a if pa <= pb and pa <= pc else b if pb <= pc else c
            row[i] = (row[i] + pick) & 255
    rows.append(row)
    prev = row
    off += stride


def ink(r, g, b):
    return r < 40 and 30 <= g <= 90 and 25 <= b <= 85 and g > r + 15


hits = []
for y, row in enumerate(rows):
    run = best = 0
    for x in range(width):
        i = x * channels
        if x >= 420 and ink(row[i], row[i + 1], row[i + 2]):
            run += 1
            best = max(best, run)
        else:
            run = 0
    if best >= 24:
        hits.append(y)

bands = []
for y in hits:
    if bands and y - bands[-1][-1] <= 2:
        bands[-1].append(y)
    else:
        bands.append([y])
bands = [band for band in bands if len(band) >= 4]
if len(bands) < 2:
    print(f"fail: selection highlight on {len(bands)} line(s), need 2")
    print(f"png: {path}")
    sys.exit(1)
gap = bands[1][0] - bands[0][-1]
if gap < 6:
    print(f"fail: the two highlight bands are only {gap}px apart")
    sys.exit(1)
print(f"ok: highlight on {len(bands)} lines, {path}")
PY
