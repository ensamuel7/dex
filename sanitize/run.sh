#!/bin/sh
set -u
modes="${*:-asan msan tsan valgrind}"
cd /src
go build -o /usr/local/bin/dex . || exit 1
status=0
for mode in $modes; do
    echo "=== $mode"
    case "$mode" in
        asan) DEX_SANITIZE=address,undefined dex test examples || status=1 ;;
        msan)
            files=$(grep -L -E 'import "(crypto|db|http|ws|redis|kafka|smtp|modules/sqlite)"|external module' examples/*_test.dx)
            DEX_SANITIZE=memory,undefined MSAN_OPTIONS=halt_on_error=1 dex test $files || status=1
            ;;
        tsan) DEX_SANITIZE=thread dex test examples || status=1 ;;
        valgrind)
            pass=0; fail=0
            for f in examples/*_test.dx; do
                b=$(basename "$f" .dx)
                if ! dex build --no-lint "$f" >/dev/null 2>&1; then
                    echo "BUILD FAIL: $f"; fail=$((fail + 1)); continue
                fi
                if valgrind -q --error-exitcode=99 --leak-check=full --errors-for-leak-kinds=definite \
                    --track-origins=yes "./build/$b" >"/tmp/$b.vg" 2>&1; then
                    pass=$((pass + 1))
                else
                    echo "FAIL: $f"; sed 's/^/  /' "/tmp/$b.vg" | head -40; fail=$((fail + 1))
                fi
                rm -f "./build/$b"
            done
            echo "valgrind: $pass passed, $fail failed"
            [ "$fail" -eq 0 ] || status=1
            ;;
        *) echo "unknown mode $mode"; status=1 ;;
    esac
done
exit $status
