#!/usr/bin/env bash
# Copies synkro-products-db's Flyway migrations into test/products-db/,
# the copy CI migrates before the integration tests run.
#
#   scripts/sync-products-db-migrations.sh          refresh the copy
#   scripts/sync-products-db-migrations.sh --check  fail if it drifted
#
# Expects synkro-products-db checked out next to this repository
# (synkro-workspace/), or its path in PRODUCTS_DB_DIR. The copy is a
# vendored snapshot because synkro-products-db is private: the workflow's
# GITHUB_TOKEN cannot check it out. Run this after every migration merged
# there, and commit the result.
set -euo pipefail

here="$(cd "$(dirname "$0")/.." && pwd)"
src="${PRODUCTS_DB_DIR:-$here/../synkro-products-db}"
dst="$here/test/products-db"
dirs=(01_ddl 02_dml 03_dcl 04_tcl)

if [ ! -f "$src/flyway.toml" ]; then
  echo "synkro-products-db not found at $src (set PRODUCTS_DB_DIR)" >&2
  exit 1
fi
sha="$(git -C "$src" rev-parse HEAD)"

if [ "${1:-}" = "--check" ]; then
  status=0
  for d in "${dirs[@]}"; do
    diff -r "$src/$d" "$dst/$d" || status=1
  done
  diff "$src/flyway.toml" "$dst/flyway.toml" || status=1
  if [ "$status" -ne 0 ]; then
    echo "test/products-db/ differs from synkro-products-db@$sha — run $0" >&2
    exit 1
  fi
  echo "test/products-db/ matches synkro-products-db@$sha"
  exit 0
fi

rm -rf "$dst"
mkdir -p "$dst"
for d in "${dirs[@]}"; do
  cp -R "$src/$d" "$dst/$d"
done
cp "$src/flyway.toml" "$dst/flyway.toml"
echo "$sha" > "$dst/SOURCE_COMMIT"
echo "Copied synkro-products-db@$sha into test/products-db/"
