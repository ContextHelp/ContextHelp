#!/usr/bin/env bash
# Regenerate the media fixtures under
# internal/providers/providertest/testdata/media/.
#
# Needs espeak-ng, ffmpeg and Python with Pillow (macOS or Linux). Fixtures are committed;
# run this only to change them, then re-record the cassettes that depend on
# them (docs/testing-media-cassettes.md).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/internal/providers/providertest/testdata/media"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$out"

line1="Welcome to the weekly review. The launch date moved to March."
line2="Understood. I will update the schedule today."

# Image: PNG with known text.
python3 - "$out/text.png" <<'PY'
import sys
from PIL import Image, ImageDraw, ImageFont
img = Image.new("RGB", (560, 140), "white")
d = ImageDraw.Draw(img)
f = ImageFont.load_default(size=40)
d.text((24, 22), "Cassette fixture", font=f, fill="black")
d.text((24, 76), "invoice 4821", font=f, fill="black")
img.save(sys.argv[1], optimize=True)
PY

# Audio: two speakers, 16 kHz mono WAV with a short gap between turns.
espeak-ng -v en-us+f3 -s 150 -w "$tmp/a.aiff" "$line1"
espeak-ng -v en-us+m3 -s 150 -w "$tmp/b.aiff" "$line2"
ffmpeg -v error -y -f lavfi -i "anullsrc=r=16000:cl=mono" -t 0.5 "$tmp/gap.wav"
ffmpeg -v error -y -i "$tmp/a.aiff" -ac 1 -ar 16000 "$tmp/a.wav"
ffmpeg -v error -y -i "$tmp/b.aiff" -ac 1 -ar 16000 "$tmp/b.wav"
ffmpeg -v error -y -i "$tmp/a.wav" -i "$tmp/gap.wav" -i "$tmp/b.wav" \
  -filter_complex "[0][1][2]concat=n=3:v=0:a=1" -ac 1 -ar 16000 \
  -c:a pcm_s16le "$out/speech.wav"

dur() { ffprobe -v error -show_entries format=duration -of csv=p=0 "$1"; }
d1="$(dur "$tmp/a.wav")"
dg="$(dur "$tmp/gap.wav")"
d2="$(dur "$tmp/b.wav")"
total="$(dur "$out/speech.wav")"

ts() { # seconds -> HH:MM:SS<sep>mmm
  python3 -c "
import sys
ms=round(float(sys.argv[1])*1000); sep=sys.argv[2]
print('%02d:%02d:%02d%s%03d' % (ms//3600000, ms//60000%60, ms//1000%60, sep, ms%1000))" "$1" "$2"
}
t2="$(python3 -c "print(float('$d1')+float('$dg'))")"
t3="$(python3 -c "print(float('$d1')+float('$dg')+float('$d2'))")"

{
  echo "1"
  echo "$(ts 0 ,) --> $(ts "$d1" ,)"
  echo "$line1"
  echo
  echo "2"
  echo "$(ts "$t2" ,) --> $(ts "$t3" ,)"
  echo "$line2"
} >"$out/speech.srt"

{
  echo "WEBVTT"
  echo
  echo "$(ts 0 .) --> $(ts "$d1" .)"
  echo "<v Speaker 1>$line1"
  echo
  echo "$(ts "$t2" .) --> $(ts "$t3" .)"
  echo "<v Speaker 2>$line2"
} >"$out/speech.vtt"

# Video: two text cards, with the speech as audio.
mkdir -p "$tmp/frames"
python3 - "$tmp/frames" "$total" <<'PY'
import sys
from PIL import Image, ImageDraw, ImageFont
d, total = sys.argv[1], float(sys.argv[2])
fps = 4
n = int(total * fps) + 1
f = ImageFont.load_default(size=34)
for i in range(n):
    card = 1 if i / fps < total / 2 else 2
    img = Image.new("RGB", (320, 180), "white" if card == 1 else "#202020")
    dr = ImageDraw.Draw(img)
    fill = "black" if card == 1 else "white"
    dr.text((20, 40), "Scene %d" % card, font=f, fill=fill)
    dr.text((20, 96), "code 7305" if card == 1 else "code 1960", font=f, fill=fill)
    img.save("%s/f%04d.png" % (d, i))
PY
ffmpeg -v error -y -framerate 4 -i "$tmp/frames/f%04d.png" -i "$out/speech.wav" \
  -c:v libx264 -preset veryslow -crf 30 -pix_fmt yuv420p -c:a aac -b:a 32k \
  -shortest -movflags +faststart -map_metadata -1 -fflags +bitexact \
  -flags:v +bitexact -flags:a +bitexact "$out/clip.mp4"

ls -l "$out"
