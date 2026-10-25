#!/bin/sh
set -u
repo=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
cd "$repo" && go build -o "$work/dex" . || exit 1
cd "$repo"
DEX_CFLAGS="-Werror -Wall -Wextra -Wconversion -Wno-sign-conversion -Wformat=2
 -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter -Wno-unused-value
 -Wno-pointer-sign -Wno-missing-field-initializers -Wno-format-nonliteral
 -Wno-unused-but-set-variable -Wno-ignored-qualifiers" \
    "$work/dex" test examples
