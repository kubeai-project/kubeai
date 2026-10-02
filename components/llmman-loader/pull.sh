#!/usr/bin/env bash
# Pull an OCI model reference through a running `llmman serve` and place it at
# the destination directory the model container mounts.
#
# The daemon does the download; it deliberately exposes no local path, so
# `llmman resolve --no-pull` is asked where the bytes landed. --no-pull
# guarantees it only reports on what the pull already fetched. It reads the
# daemon's store, so LLMMAN_MODELS must point at the same store.
set -euo pipefail

usage="usage: pull <oci-reference> <destination>"
reference="${1:?$usage}"
destination="${2:?$usage}"

host="${LLMMAN_HOST:-127.0.0.1:17434}"
# A client cannot connect to "every interface".
host="${host#*://}"
host="${host%%/*}"
case "$host" in
  0.0.0.0:*) host="127.0.0.1:${host##*:}" ;;
  "[::]:"*)  host="[::1]:${host##*:}" ;;
esac
base="http://${host}"

echo "Checking for an llmman daemon at ${base}"
if ! curl -fsS --max-time 5 "${base}/api/version" | grep -q '"version"'; then
  echo "No llmman daemon reachable at ${base}. Start one with 'llmman serve', or set LLMMAN_HOST." >&2
  exit 1
fi

echo "Pulling ${reference}"
# The daemon streams NDJSON status objects and reports failures in-band at
# HTTP 200, so the stream is inspected rather than trusting the status code.
status_file="$(mktemp)"
trap 'rm -f "$status_file"' EXIT
curl -fsS -N -X POST "${base}/api/pull" \
  -H 'Content-Type: application/json' \
  -d "{\"model\":\"${reference}\"}" | tee "$status_file"

if grep -q '"error"' "$status_file"; then
  echo "llmman pull of ${reference} failed" >&2
  exit 1
fi
if ! grep -q '"status":"success"' "$status_file"; then
  echo "llmman pull of ${reference} ended without reporting success" >&2
  exit 1
fi

resolved="$(llmman resolve --no-pull "${reference}" | tail -n1 | sed -n 's/.*"path":"\([^"]*\)".*/\1/p')"
if [ -z "$resolved" ] || [ ! -e "$resolved" ]; then
  echo "llmman resolve did not report a usable path for ${reference}" >&2
  exit 1
fi

mkdir -p "$destination"
# A directory is copied by contents. -L dereferences llmman's links into
# regular files.
[ -d "$resolved" ] && resolved="$resolved/."
cp -aL "$resolved" "$destination/"

echo "Placed ${reference} at ${destination}"
