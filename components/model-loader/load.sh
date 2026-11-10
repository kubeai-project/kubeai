#!/bin/bash

set -euxo pipefail

src=$1
dest=$2

# Check both URLs before starting a credentialed command. Quoting below is
# still required: URLs and local paths must always remain single arguments.
validate_url() {
    local url=$1
    if [[ $url == *[[:space:]]* || ! $url =~ ^(hf|s3|gs|oss)://[^/]+ ]]; then
        echo "Invalid loader URL" >&2
        exit 1
    fi
    # hf receives the repository without its scheme, so reject option names.
    if [[ $url == hf://-* ]]; then
        echo "Invalid Hugging Face repository" >&2
        exit 1
    fi
}

validate_url "$src"
if [[ $dest == *"://"* ]]; then
    validate_url "$dest"
fi

# If dest is a local directory, download the model to that directory.
# Otherwise, download to a temporary directory and upload from there.
dest_type=""
if [[ $dest == *"://"* ]]; then
    dir=$(mktemp -d)
    dest_type="url"
else
    dir=$dest
    dest_type="dir"
    mkdir -p -- "$dir"
fi

# Download
case $src in
    "hf://"*)
        repo=${src#hf://}
        hf download --local-dir "$dir" "$repo"
        rm -rf -- "$dir/.cache"
        ;;
    "s3://"*)
        aws s3 sync "$src" "$dir"
        ;;
    "gs://"*)
        gcloud auth activate-service-account --key-file "$GOOGLE_APPLICATION_CREDENTIALS"
        gcloud storage rsync "$src" "$dir"
        ;;
    "oss://"*)
        ossutil sync "$src" "$dir"
        ;;
    *)
        echo "Unsupported source url: $src"
        exit 1
        ;;
esac

# Upload
if [[ $dest_type == "url" ]]; then
    case $dest in
        "hf://"*)
            repo=${dest#hf://}
            hf upload "$repo" "$dir"
            ;;
        "s3://"*)
            aws s3 sync "$dir" "$dest"
            ;;
        "gs://"*)
            gcloud auth activate-service-account --key-file "$GOOGLE_APPLICATION_CREDENTIALS"
            gcloud storage rsync "$dir" "$dest"
            ;;
        "oss://"*)
            ossutil sync "$dir" "$dest"
            ;;
        *)
            echo "Unsupported destination url: $dest"
            exit 1
            ;;
    esac
fi
