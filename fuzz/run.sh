#!/bin/sh
set -eu

here=$(cd "$(dirname "$0")" && pwd)
targets_all="json http ws_frame ws_handshake base64 str"
seconds=120
docker_mode=0
targets=""
extra=""

while [ $# -gt 0 ]; do
    case "$1" in
        --docker) docker_mode=1 ;;
        -t|--time) seconds="$2"; shift ;;
        --time=*) seconds="${1#--time=}" ;;
        --) shift; extra="$*"; break ;;
        -h|--help)
            echo "usage: fuzz/run.sh [--docker] [-t SECONDS] [target ...] [-- fuzzer flags]"
            echo "targets: $targets_all"
            exit 0 ;;
        *) targets="$targets $1" ;;
    esac
    shift
done
[ -n "$targets" ] || targets=$targets_all

libs_for() {
    case "$1" in
        http) echo "-lcurl -lpthread" ;;
        *) echo "-lpthread" ;;
    esac
}

if [ "$docker_mode" = 1 ] && [ "${DEX_FUZZ_IN_DOCKER:-0}" != 1 ]; then
    out=${DEX_FUZZ_OUT:-$here/../build/fuzz-docker}
    mkdir -p "$out"
    docker build -q -t dex-fuzz "$here" >/dev/null
    exec docker run --rm -e DEX_FUZZ_IN_DOCKER=1 -e DEX_FUZZ_OUT=/out \
        -v "$here/..":/src:ro -v "$out":/out dex-fuzz \
        sh /src/fuzz/run.sh --docker -t "$seconds" $targets -- $extra
fi

out=${DEX_FUZZ_OUT:-${TMPDIR:-/tmp}/dex-fuzz}
mkdir -p "$out"
san="-fsanitize=address,undefined -fno-sanitize-recover=undefined"
cflags="-g -O1 -fno-omit-frame-pointer -DDEX_SSL_DISABLED"
status=0

for t in $targets; do
    src="$here/$t.c"
    [ -f "$src" ] || { echo "no such target: $t" >&2; exit 2; }
    bin="$out/$t"
    work="$out/corpus/$t"
    arts="$out/artifacts/$t/"
    mkdir -p "$work" "$arts"
    dict=""
    [ -f "$here/dict/$t.dict" ] && dict="-dict=$here/dict/$t.dict"
    if [ "$docker_mode" = 1 ]; then
        clang $cflags $san -fsanitize=fuzzer "$src" -o "$bin" $(libs_for "$t")
        echo "== $t: libFuzzer for ${seconds}s"
        "$bin" -max_total_time="$seconds" -timeout=10 -max_len=65536 -print_final_stats=1 \
            -artifact_prefix="$arts" $dict $extra "$work" "$here/corpus/$t" || status=1
    else
        clang $cflags $san -fsanitize-coverage=trace-pc-guard -c "$src" -o "$out/$t.o"
        clang $cflags $san "$out/$t.o" "$here/driver.c" -o "$bin" $(libs_for "$t")
        echo "== $t: driver for ${seconds}s"
        "$bin" -max_total_time="$seconds" -timeout=10 -max_len=65536 \
            -artifact_prefix="$arts" $dict $extra "$here/corpus/$t" || status=1
    fi
done

[ "$status" = 0 ] && echo "all targets ran clean" || echo "crashes under $out/artifacts" >&2
exit $status
