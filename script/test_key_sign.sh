#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

grpcurl -plaintext -max-time 600 -format json -d '

{
  "sign_id": "test-sign-id",
  "key_id": "test-key",
  "sign_msg": "MTIzCg=="
}

' 127.0.0.1:9001 tss.TssService/KeySign
