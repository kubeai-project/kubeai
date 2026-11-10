#!/usr/bin/env bash
# Exercise the real loader with credential-free CLI stubs.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
t="$(mktemp -d)"
trap 'rm -rf "$t"' EXIT
mkdir "$t/bin" "$t/tmp"
cat >"$t/bin/stub" <<'STUB'
#!/usr/bin/env bash
printf '%s\0' "${0##*/}" "$@" >>"$STUB_LOG"
STUB
chmod +x "$t/bin/stub"
for cli in hf aws gcloud ossutil; do
    ln -s stub "$t/bin/$cli"
done
export PATH="$t/bin:$PATH" STUB_LOG="$t/log" TMPDIR="$t/tmp"
export GOOGLE_APPLICATION_CREDENTIALS="$t/cloud credentials.json"

reject() {
    : >"$STUB_LOG"
    if bash "$here/load.sh" "$1" "$2" >"$t/out" 2>&1; then
        echo "FAIL: accepted invalid URL" >&2
        cat "$t/out"
        exit 1
    fi
    # Invalid uploads must fail before even downloading the source.
    test ! -s "$STUB_LOG"
}

for scheme in hf s3 gs oss; do
    for suffix in ' --endpoint-url http://evil.example' $'\t--config=evil' $'\n--local-dir=/tmp/evil'; do
        reject "$scheme://bucket/model$suffix" "$t/dest"
        reject 'hf://org/model' "$scheme://bucket/model$suffix"
    done
    reject "$scheme://" "$t/dest"
    reject 'hf://org/model' "$scheme://"
done
for url in hf://--help hf://--local-dir=/tmp/evil; do
    reject "$url" "$t/dest"
    reject 'hf://org/model' "$url"
done

expect() {
    printf '%s\0' "$@" >>"$t/expected"
}
check() {
    if ! cmp -s "$STUB_LOG" "$t/expected"; then
        echo 'FAIL: command arguments changed' >&2
        cat "$t/out"
        exit 1
    fi
}

# Spaces and glob characters in local paths must survive as literal bytes.
dir="$t/dest with spaces [*]"
for scheme in hf s3 gs oss; do
    : >"$STUB_LOG"
    : >"$t/expected"
    src="$scheme://bucket/model"
    case "$scheme" in
        hf) expect hf download --local-dir "$dir" bucket/model ;;
        s3) expect aws s3 sync "$src" "$dir" ;;
        gs)
            expect gcloud auth activate-service-account --key-file "$GOOGLE_APPLICATION_CREDENTIALS"
            expect gcloud storage rsync "$src" "$dir"
            ;;
        oss) expect ossutil sync "$src" "$dir" ;;
    esac
    bash "$here/load.sh" "$src" "$dir" >"$t/out" 2>&1
    test -d "$dir"
    check

    # Exercise uploads, including the temporary download directory.
    : >"$STUB_LOG"
    : >"$t/expected"
    bash "$here/load.sh" 'hf://org/model' "$src" >"$t/out" 2>&1
    # mktemp creates exactly one directory per upload.
    dirs=("$t/tmp/"*)
    temp_dir=${dirs[0]}
    expect hf download --local-dir "$temp_dir" org/model
    case "$scheme" in
        hf) expect hf upload bucket/model "$temp_dir" ;;
        s3) expect aws s3 sync "$temp_dir" "$src" ;;
        gs)
            expect gcloud auth activate-service-account --key-file "$GOOGLE_APPLICATION_CREDENTIALS"
            expect gcloud storage rsync "$temp_dir" "$src"
            ;;
        oss) expect ossutil sync "$temp_dir" "$src" ;;
    esac
    check
    rm -rf -- "$temp_dir"
done

# A cloud object glob is data, even if matching files exist locally.
mkdir -p "$t/work/s3:/bucket"
touch "$t/work/s3:/bucket/model-one"
: >"$STUB_LOG"
: >"$t/expected"
expect aws s3 sync 's3://bucket/model-*' "$dir"
(cd "$t/work" && bash "$here/load.sh" 's3://bucket/model-*' "$dir") >"$t/out" 2>&1
check
echo 'All model-loader tests passed'
