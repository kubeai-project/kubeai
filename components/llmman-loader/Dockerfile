FROM alpine:3.24

# `llmman resolve --no-pull` reports where the daemon's pull landed on disk;
# the daemon itself does all registry work, so nothing else is needed here.
RUN apk add --no-cache curl ca-certificates bash coreutils
RUN curl -fsSL https://raw.githubusercontent.com/llmmanorg/llmman/main/install.sh | sh
RUN llmman --version

COPY ./pull.sh /bin/pull
RUN chmod +x /bin/pull
ENTRYPOINT ["/bin/pull"]
