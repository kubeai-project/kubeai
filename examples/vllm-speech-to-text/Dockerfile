FROM vllm/vllm-openai:v0.31.0

# Speech-to-text needs the optional audio dependencies. Constrain packages
# already in the image so installing extras preserves its compiled CUDA stack.
RUN uv pip list --system --format freeze > /tmp/vllm-base-constraints.txt \
    && uv pip install --system --constraint /tmp/vllm-base-constraints.txt "vllm[audio]==0.31.0" \
    && python3 -c "import av, scipy, soundfile, soxr" \
    && rm /tmp/vllm-base-constraints.txt
