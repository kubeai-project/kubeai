#!/bin/bash

set -euxo pipefail

# run.sh <testcase> [skaffold flags]

testcase=$1
shift # All additional arguments are passed to skaffold.
skaffold_flags=$@

export REPO_DIR=$(git rev-parse --show-toplevel)
export TEST_DIR=$REPO_DIR/test/e2e/$testcase
export TMP_DIR=$REPO_DIR/tmp
export PATH=$REPO_DIR/bin:$PATH
export DOCKER_BUILDKIT=1

mkdir -p $REPO_DIR/tmp

source $REPO_DIR/test/e2e/common.sh

skaffold_log_file=$TMP_DIR/skaffold.log

# Avoid using an unintended kubectl context.
expected_kubectl_context=${TEST_KUBECTL_CONTEXT:-kind-kind}
current_kubectl_context=$(kubectl config current-context)
if [ "$current_kubectl_context" != "$expected_kubectl_context" ]; then
    output "Current kubectl context is $current_kubectl_context, expected $expected_kubectl_context"
    output "Set TEST_KUBECTL_CONTEXT to override the expected context."
    exit 1
fi

# Function to handle errors
error_handler() {
    echo "Tests failed. Printing logs..."
    if [ -f $skaffold_log_file ]; then
        echo "--- Skaffold Logs ---"
        cat $skaffold_log_file
    fi
    echo "--- Nodes ---"
    kubectl get nodes -owide
    echo "--- Pods ---"
    kubectl get pods -owide
    echo "--- Events ---"
    kubectl get events
    echo "--- Models ---"
    kubectl get crds models.kubeai.org && kubectl get models -oyaml
    echo "--- Model Logs ---"
    kubectl logs -l app=model --tail -1
    echo "--- Describe Model Pods ---"
    kubectl describe pod -l app=model
    echo "!!! FAIL !!!"
    exit 1
}
trap 'error_handler' ERR

skaffold_pid=9999999999999999
cleanup() {
    echo "Running test framework cleanup..."
    if [ $skaffold_pid != 9999999999999999 ]; then
        kill $skaffold_pid
        wait $skaffold_pid
    fi
    skaffold delete -f $REPO_DIR/skaffold.yaml
}
trap cleanup EXIT

# Assert that PATH is configured correctly.
if [ "$(which skaffold)" != "$REPO_DIR/bin/skaffold" ]; then
    echo "Tools are not set up correctly. Probably need to run via Makefile."
    exit 1
fi

skaffold_build_file=$TMP_DIR/skaffold-build.json
skaffold build -f $REPO_DIR/skaffold.yaml --file-output=$skaffold_build_file $skaffold_flags
skaffold deploy -f $REPO_DIR/skaffold.yaml --tail --port-forward --load-images --build-artifacts=$skaffold_build_file $skaffold_flags > $skaffold_log_file &
skaffold_pid=$!

echo "Waiting for KubeAI API on localhost:8000"
retry 600 curl -s http://localhost:8000/openai/v1/models
echo "KubeAI API is ready"

# Apply common manifests.
kubectl apply -f $REPO_DIR/test/e2e/common-manifests.yaml

$REPO_DIR/test/e2e/$testcase/test.sh

echo "!!! PASS !!!"