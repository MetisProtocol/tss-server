#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

# building an image for two platforms
docker build -t "tss-server:latest" .