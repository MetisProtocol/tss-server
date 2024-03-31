# syntax=docker/dockerfile:1
FROM golang:1.20 as builder
RUN apt update && apt install -y build-essential git
WORKDIR /app
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go install .

FROM debian:12-slim
COPY --from=builder /go/bin/* /usr/local/bin/
EXPOSE 8081 4001
ENTRYPOINT ["tss-server"]
