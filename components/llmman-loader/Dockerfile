# The llmman release binary is glibc-linked, so not Alpine.
FROM debian:stable-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# `llmman resolve --no-pull` reports where the daemon's pull landed on disk;
# the daemon itself does all registry work.
ARG LLMMAN_VERSION=v0.1.507
RUN curl -fsSL -o /usr/local/bin/llmman \
    "https://github.com/llmmanorg/llmman/releases/download/${LLMMAN_VERSION}/llmman-$(uname -m)-unknown-linux-gnu" \
    && chmod +x /usr/local/bin/llmman
RUN llmman --version

COPY ./pull.sh /bin/pull
RUN chmod +x /bin/pull
ENTRYPOINT ["/bin/pull"]
