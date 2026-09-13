#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Run against one disposable Redis/Valkey endpoint at a time. No FLUSHDB is used.
set -eu
: "${ACOR_V3_SCALE_ADDR:?set ACOR_V3_SCALE_ADDR to a disposable real server}"
repeats=${ACOR_V3_SCALE_REPEATS:-3}
output=${ACOR_V3_SCALE_OUTPUT:-/tmp/acor-v3-results}
counts=${ACOR_V3_SCALE_COUNTS:-"10000 100000 1000000"}
modes=${ACOR_V3_SCALE_MODES:-"baseline"}
compare=${ACOR_V3_DELTA_COMPARE:-0}
mkdir -p "$output"
binary=$(mktemp /tmp/acor-v3-test.XXXXXX)
trap 'rm -f "$binary"' EXIT HUP INT TERM
go test -c ./pkg/acor -o "$binary"
repeat=1
while [ "$repeat" -le "$repeats" ]; do
  for count in $counts; do
    for kind in shared diverse korean; do
      for mode in $modes; do
        case "$mode" in
          baseline) delta=0 ;;
          delta) delta=1 ;;
          *) echo "invalid mode: $mode" >&2; exit 2 ;;
        esac
        if [ "$compare" = 1 ]; then
          test_name=TestVersionedDeltaScale
          result="$output/$count-$kind-$mode-$repeat.txt"
        else
          test_name=TestVersionedScale
          result="$output/$count-$kind-$repeat.txt"
        fi
        ACOR_V3_SCALE_N=$count ACOR_V3_SCALE_KIND=$kind ACOR_V3_SCALE_DELTA=$delta \
          "$binary" -test.run="^$test_name\$" -test.v -test.timeout=15m > "$result" 2>&1
        echo "completed $count $kind $mode repeat $repeat"
      done
    done
  done
  repeat=$((repeat + 1))
done
if [ "$compare" != 1 ]; then
  "$binary" -test.run='^TestVersionedMillionSafety$' -test.v -test.timeout=15m \
    > "$output/million-safety.txt" 2>&1
fi
