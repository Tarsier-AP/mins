#!/usr/bin/env bash
# Generate the synthetic media used by the integration tests.
set -euo pipefail

root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
out="$root/testdata/generated"
mkdir -p "$out"

gen() {
  local name=$1
  shift
  if [[ -s $out/$name ]]; then
    return
  fi
  echo "fixtures: $name"
  ffmpeg -hide_banner -loglevel error -y "$@" "$out/$name"
}

gen in.png    -f lavfi -i testsrc2=s=320x240:d=1 -frames:v 1
gen anim.gif  -f lavfi -i testsrc2=s=160x120:d=1:r=5 -vf fps=5
gen clip.mp4  -f lavfi -i testsrc2=s=160x120:d=1:r=15 \
              -f lavfi -i sine=frequency=440:duration=1 \
              -c:v libx264 -pix_fmt yuv420p -c:a aac -shortest
gen tone.flac -f lavfi -i sine=frequency=440:duration=2 -c:a flac

echo "fixtures: $out"
