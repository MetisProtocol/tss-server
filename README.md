# Multi-Party Threshold Signature Scheme

[![MIT licensed][1]][2]

[1]: https://img.shields.io/badge/license-MIT-blue.svg
[2]: LICENSE

Permissively MIT Licensed.

## Introduction

This is TSS server for Metis decentralized sequencer, uses bnb-chain/tss-lib.

## Usage

This program relies on Docker and Docker Compose.

### Build

```bash
# Ubuntu
./script/build_docker.sh

# MacOSX
./script/buildx_docker.sh
```

### Keygen

```bash
# run docker compose first
docker-compose up -d

# get all localParty
./test_get_local_party_id.sh

# replace all_party_id > id in test_key_gen.sh with above result

# run KeyGen
./test_key_gen.sh
```

### Signing

```bash
./test_loop_sign_verify.sh
```
