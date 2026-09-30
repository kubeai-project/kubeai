#!/bin/bash

source $REPO_DIR/test/e2e/common.sh

model=qwen2-500m-instruct-sglang-cpu

# The -xeon image compiles its CPU kernels with -march=x86-64-v4 -mavx512bf16
# -mavx512vnni and exits with SIGILL (code 132) on CPUs without these.
required_cpu_flags="avx512f avx512bw avx512cd avx512dq avx512vl avx512_bf16 avx512_vnni"
kind_container=$(docker ps --filter "name=kind-control-plane" --format "{{.ID}}")
cpu_flags=" $(docker exec $kind_container grep -m1 '^flags' /proc/cpuinfo) "
missing_cpu_flags=""
for flag in $required_cpu_flags; do
  [[ "$cpu_flags" == *" $flag "* ]] || missing_cpu_flags="$missing_cpu_flags $flag"
done
if [ -n "$missing_cpu_flags" ]; then
  echo "::warning::Skipping engine-sglang: the CPU lacks$missing_cpu_flags"
  exit 0
fi

apply_model $model

response_file=$TMP_DIR/completion.json
curl http://localhost:8000/openai/v1/chat/completions \
  --max-time 900 \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'$model'",
    "messages": [{"role": "user", "content": "Who was the first president of the United States?"}],
    "max_tokens": 20
  }' > $response_file

content=$(cat $response_file | jq -r '.choices[0].message.content')
if [ -z "$content" ] || [ "$content" == "null" ]; then
  echo "Empty completion"
  cat $response_file
  exit 1
fi

echo "Successfully generated a completion: $content"
