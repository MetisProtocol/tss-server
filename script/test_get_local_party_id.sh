#!/bin/sh

DIR=$(cd "$(dirname "$0")/.." && pwd)

grpcurl -plaintext -max-time 600 -format json -d '

{
}

' 127.0.0.1:9001 tss.TssService/GetLocalPartyId

grpcurl -plaintext -max-time 600 -format json -d '

{
}

' 127.0.0.1:9002 tss.TssService/GetLocalPartyId

grpcurl -plaintext -max-time 600 -format json -d '

{
}

' 127.0.0.1:9003 tss.TssService/GetLocalPartyId