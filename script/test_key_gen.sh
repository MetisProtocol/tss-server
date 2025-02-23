#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

grpcurl -plaintext -max-time 600 -format json -d '

{
  "key_id": "test-key",
  "threshold": 1,
  "all_party_id": [
    {
      "id": "16Uiu2HAm9xRGwnVKVey2e7qApBMVQhUj4UMoVXjFeaKusA7KxxYx",
      "moniker": "0xD10981143992640821dA028d0FbFF44B034b7EB1",
      "key": "Atfj/vFRixCv1kRdHtEJgRQ5kmQIIdoCjQ+/9EsDS36x"
    },
    {
      "id": "16Uiu2HAmPZAoSHfYWg9W4WmJJDWv4Cyf1oYFVWN6ULo8t7LCdvX2",
      "moniker": "0xF40D9504C9A99E66adfA3D455Db25F5668B89B71",
      "key": "A6Hy41q3QViMXTiBNvQNlQTJqZ5mrfo9RV2yX1ZouJtx"
    },
    {
      "id": "16Uiu2HAm4z7HSPpKrCBqcDX7bhx2K195TZfjEYKBe9M4ioDJgno5",
      "moniker": "0xe79D905C5F78Bc493B1e70F43269cF2BA2D2186C",
      "key": "Ao4IYTsM43Q+twPT4uedkFxfeLxJOx5w9DJpzyui0hhs"
    }
  ]
}

' 127.0.0.1:9001 tss.TssService/KeyGen
