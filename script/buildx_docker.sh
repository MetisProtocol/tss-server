#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

docker buildx build --no-cache --output type=docker -t "tss-server:latest" .
