#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
RUNS="${1:-3}"

BENCHMARKS="loop_count fib_recursive fib_iterative string_concat array_ops struct_ops"

BUILD_DIR="$SCRIPT_DIR/build"
mkdir -p "$BUILD_DIR"

# Colors
BOLD='\033[1m'
CYAN='\033[36m'
GREEN='\033[32m'
YELLOW='\033[33m'
RESET='\033[0m'

printf "${BOLD}DexLang Benchmark Suite${RESET}\n"
echo "Runs per benchmark: $RUNS"
echo ""

# ---------- Compile ----------

printf "${CYAN}Compiling C benchmarks (gcc -O2)...${RESET}\n"
for bench in $BENCHMARKS; do
    gcc -O2 -o "$BUILD_DIR/${bench}_c" "$SCRIPT_DIR/c/${bench}.c"
done
echo "  done."

printf "${CYAN}Compiling DexLang benchmarks (dex build)...${RESET}\n"
for bench in $BENCHMARKS; do
    (cd "$PROJECT_DIR" && go run . build "benchmarks/dex/${bench}.dx") > /dev/null
    mv "$PROJECT_DIR/build/${bench}" "$BUILD_DIR/${bench}_dex"
done
echo "  done."

echo ""

# ---------- Run ----------

# measure_ms: run a binary and return wall-clock time in ms
measure_ms() {
    local bin="$1"
    python3 -c "
import subprocess, time
start = time.monotonic_ns()
subprocess.run(['$bin'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
end = time.monotonic_ns()
print((end - start) / 1_000_000)
"
}

# median of a list of numbers
median() {
    python3 -c "
import sys
nums = sorted(float(x) for x in sys.argv[1:])
n = len(nums)
if n % 2 == 1:
    print(nums[n // 2])
else:
    print((nums[n // 2 - 1] + nums[n // 2]) / 2)
" "$@"
}

# Store results in temp files
RESULTS_DIR=$(mktemp -d)
trap "rm -rf $RESULTS_DIR $BUILD_DIR" EXIT

for bench in $BENCHMARKS; do
    printf "${YELLOW}Running: ${bench}${RESET}\n"

    # C
    times_c=""
    for r in $(seq 1 "$RUNS"); do
        t=$(measure_ms "$BUILD_DIR/${bench}_c")
        times_c="$times_c $t"
        printf "  C    run %d: %s ms\n" "$r" "$t"
    done
    median $times_c > "$RESULTS_DIR/${bench}_c"

    # DexLang
    times_dex=""
    for r in $(seq 1 "$RUNS"); do
        t=$(measure_ms "$BUILD_DIR/${bench}_dex")
        times_dex="$times_dex $t"
        printf "  Dex  run %d: %s ms\n" "$r" "$t"
    done
    median $times_dex > "$RESULTS_DIR/${bench}_dex"

    echo ""
done

# ---------- Results ----------

printf "${BOLD}Results (median of $RUNS runs, in ms)${RESET}\n"
echo ""
printf "%-20s %12s %12s %10s\n" "Benchmark" "C (ms)" "Dex (ms)" "Ratio"
printf "%-20s %12s %12s %10s\n" "--------------------" "------------" "------------" "----------"

for bench in $BENCHMARKS; do
    c_ms=$(cat "$RESULTS_DIR/${bench}_c")
    dex_ms=$(cat "$RESULTS_DIR/${bench}_dex")
    ratio=$(python3 -c "
c = float('$c_ms')
d = float('$dex_ms')
if c > 0:
    print(f'{d/c:.2f}x')
else:
    print('N/A')
")
    printf "%-20s %12.1f %12.1f %10s\n" "$bench" "$c_ms" "$dex_ms" "$ratio"
done

echo ""
printf "${GREEN}Done.${RESET}\n"
