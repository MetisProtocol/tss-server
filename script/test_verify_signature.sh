#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

grpcurl -plaintext -max-time 600 -format json -d '

{
  "sign_msg": "MTIzCg==",
  "public_key": "AuSuZ2afOFmIvpyySvI4rU9xijWrKetO6ad7C6641o8P",
  "signature_r": "YgkMru4R8wJjN4ggokQ3eoM3zLzf0ga/TbEEVHl9eZI=",
  "signature_s": "PSir+ox6iaiYtAPVRcemnhXFRXmQeLqylRX3w/cATNU="
}

' 127.0.0.1:9001 tss.TssService/VerifySignature