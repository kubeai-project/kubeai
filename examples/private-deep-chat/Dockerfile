FROM golang:1.25 AS builder

WORKDIR /workspace
COPY go.* .

RUN go mod download

COPY main.go main.go
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o server ./main.go

FROM gcr.io/distroless/static:nonroot

WORKDIR /app
COPY --from=builder /workspace/server /app/
COPY ./static /app/static
USER 65532:65532

ENTRYPOINT ["/app/server"]
