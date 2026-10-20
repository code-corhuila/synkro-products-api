#!/usr/bin/env bash
# Applies test/products-db/ (synkro-products-db's migrations) with the
# same Flyway image synkro-products-db's own CI and deploy/compose.yml
# run, then checks that every V migration was applied — a missing
# location makes Flyway "succeed" with zero migrations.
#
# Needs FLYWAY_URL, FLYWAY_USER and FLYWAY_PASSWORD (an admin role: the
# migrations create a schema and a role), and FLYWAY_DOCKER_NETWORK when
# the database is not reachable on the host network (default: host).
set -euo pipefail

: "${FLYWAY_URL:?}" "${FLYWAY_USER:?}" "${FLYWAY_PASSWORD:?}"
image="flyway/flyway:11.20.3-alpine" # keep identical to synkro-products-db
# Git Bash on Windows rewrites "/workspace" arguments into Windows paths
# (and Flyway then finds zero migrations); this keeps them literal.
export MSYS_NO_PATHCONV=1
dir="$(cd "$(dirname "$0")/../test/products-db" && (pwd -W 2>/dev/null || pwd))"

# docker cp instead of a bind mount, as in synkro-products-db's CI: it
# works whether or not the Docker daemon can see this filesystem.
cid=$(docker create --network "${FLYWAY_DOCKER_NETWORK:-host}" \
  -e FLYWAY_URL -e FLYWAY_USER -e FLYWAY_PASSWORD \
  "$image" -workingDirectory=/workspace -configFiles=flyway.toml migrate)
trap 'docker rm -f "$cid" > /dev/null' EXIT
docker cp -q "$dir/." "$cid:/workspace"
docker start -a "$cid"
exit_code="$(docker inspect -f '{{.State.ExitCode}}' "$cid")"
[ "$exit_code" -eq 0 ] || exit "$exit_code"

expected=$(find "$dir" -name 'V*.sql' | wc -l | tr -d ' ')
echo "V migrations in test/products-db: $expected"
