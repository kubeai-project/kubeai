#!/usr/bin/env bash
# Tests pull.sh against stub `curl` and `llmman` commands.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT
mkdir "$t/bin" "$t/model"
echo weights >"$t/model/model.safetensors"
echo '{}' >"$t/model/config.json"

# Stub curl: logs its URL; /api/version fails if STUB_NO_DAEMON is set,
# /api/pull streams $STUB_STREAM.
cat >"$t/bin/curl" <<'STUB'
#!/usr/bin/env bash
for a; do case "$a" in http*) url="$a" ;; esac; done
echo "$url" >>"$STUB_LOG"
case "$url" in
  */api/version) [ -z "${STUB_NO_DAEMON:-}" ] && echo '{"version":"0.1"}' ;;
  */api/pull) printf '%s\n' "$STUB_STREAM" ;;
esac
STUB
# Stub llmman: `resolve` reports $STUB_PATH, or fails if it is unset.
cat >"$t/bin/llmman" <<'STUB'
#!/usr/bin/env bash
[ "$1 $2" = "resolve --no-pull" ] || exit 2
[ -n "${STUB_PATH:-}" ] || exit 1
echo "{\"reference\":\"$3\",\"path\":\"$STUB_PATH\",\"format\":\"safetensors\"}"
STUB
chmod +x "$t/bin/curl" "$t/bin/llmman"

ok='{"status":"pulling manifest"}
{"status":"success"}'
failures=0

# run <name> <want: ok|fail> [VAR=value ...]; destination is $t/dest.
run() {
  local name="$1" want="$2" rc=0
  shift 2
  rm -rf "$t/dest"
  : >"$t/log"
  env PATH="$t/bin:$PATH" STUB_LOG="$t/log" STUB_STREAM="$ok" STUB_PATH="$t/model" "$@" \
    "$here/pull.sh" example.com/org/model:tag "$t/dest" >"$t/out" 2>&1 || rc=$?
  if { [ "$want" = ok ] && [ $rc -eq 0 ]; } || { [ "$want" = fail ] && [ $rc -ne 0 ]; }; then
    echo "PASS $name"
  else
    echo "FAIL $name (want $want, exit $rc)"
    cat "$t/out"
    failures=$((failures + 1))
  fi
}

run copies-directory ok
[ "$(cat "$t/dest/model.safetensors")" = weights ] || { echo "FAIL files not copied"; failures=$((failures + 1)); }
[ -f "$t/dest/config.json" ] || { echo "FAIL config.json not copied"; failures=$((failures + 1)); }

run copies-single-file ok STUB_PATH="$t/model/model.safetensors"
[ -f "$t/dest/model.safetensors" ] || { echo "FAIL single file not copied"; failures=$((failures + 1)); }

run default-host ok
grep -qx 'http://127.0.0.1:17434/api/pull' "$t/log" || { echo "FAIL default host"; failures=$((failures + 1)); }

run wildcard-host ok LLMMAN_HOST=0.0.0.0:9
grep -qx 'http://127.0.0.1:9/api/pull' "$t/log" || { echo "FAIL wildcard host"; failures=$((failures + 1)); }

run url-host ok LLMMAN_HOST=http://llmman.svc:1/
grep -qx 'http://llmman.svc:1/api/pull' "$t/log" || { echo "FAIL url host"; failures=$((failures + 1)); }

run no-daemon fail STUB_NO_DAEMON=1
run in-band-error fail STUB_STREAM='{"error":"unauthorized"}'
run error-then-success fail STUB_STREAM='{"error":"blob missing"}
{"status":"success"}'
run no-success fail STUB_STREAM='{"status":"pulling manifest"}'
run resolve-fails fail STUB_PATH=
run resolve-missing-path fail STUB_PATH="$t/nope"
[ ! -e "$t/dest" ] || { echo "FAIL destination created on failure"; failures=$((failures + 1)); }

[ $failures -eq 0 ] || { echo "$failures failure(s)"; exit 1; }
echo "all passed"
