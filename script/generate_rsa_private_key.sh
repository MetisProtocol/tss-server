#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

openssl genrsa -out rsa_private.pem 2048