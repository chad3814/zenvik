#!/bin/sh
# Regenerates udf/testdata/hdiutil-udf102.iso with macOS hdiutil (UDF 1.02).
# The file contents must match udf/hdiutil_fixture_test.go.
set -eu
cd "$(dirname "$0")/.."
src=$(mktemp -d)
trap 'rm -rf "$src"' EXIT
mkdir -p "$src/BDMV/PLAYLIST" "$src/BDMV/STREAM"
printf 'MPLS0200' > "$src/BDMV/PLAYLIST/00800.mpls"
printf 'hello from hdiutil\n' > "$src/BDMV/hello.txt"
head -c 300000 /dev/zero | LC_ALL=C tr '\0' 'z' > "$src/BDMV/STREAM/00001.m2ts"
mkdir -p udf/testdata
rm -f udf/testdata/hdiutil-udf102.iso
hdiutil makehybrid -quiet -udf -udf-version 1.02 -udf-volume-name ZENVIK_HDIUTIL \
  -o udf/testdata/hdiutil-udf102.iso "$src"
ls -l udf/testdata/hdiutil-udf102.iso
