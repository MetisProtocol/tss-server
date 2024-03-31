#!/bin/sh

urls=(
  "127.0.0.1:9001"
  "127.0.0.1:9002"
  "127.0.0.1:9003"
)

for i in $(seq 1 1000); do
  index=$((RANDOM % ${#urls[@]}))

  url=${urls[$index]}

  # sign
  echo "posting No.${i} grpcurl ${url}..."
  grpcurl -plaintext -max-time 600 -format json -d '

    {
    "sign_id": "test-sign-id-${i}",
    "key_id": "test-key",
    "sign_msg": "MTIzCg=="
    }

  ' $url tss.TssService/KeySign

  # sleep 20 seconds
  echo "sleep 20 seconds..."
  sleep 20

  # check
  echo "checking No.${i} grpcurl ${url}..."
  grpcurl -plaintext -max-time 600 -format json -d '

    {
    "sign_msg": "MTIzCg==",
    "public_key": "AuSuZ2afOFmIvpyySvI4rU9xijWrKetO6ad7C6641o8P",
    "signature_r": "YgkMru4R8wJjN4ggokQ3eoM3zLzf0ga/TbEEVHl9eZI=",
    "signature_s": "PSir+ox6iaiYtAPVRcemnhXFRXmQeLqylRX3w/cATNU="
    }

  ' $url tss.TssService/VerifySignature

done

echo "loop finished."
