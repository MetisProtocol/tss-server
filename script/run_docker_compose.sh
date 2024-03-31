#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

cd $DIR/local_test && docker-compose up
