# create-chain

CLI tool to create an Avalanche chain on Fuji or Mainnet.

## Prerequisites

1. **Funded P-Chain Address**: You need a P-Chain address with AVAX
    - This tool assummed wallet is stored in platform-cli keystore.

2. **Running Validator Nodes**: Your validators must be running and synced with the network

3. **Go 1.24.13+**: Required to build the tool
4. **Existence of subnet+**: 

## Build

go build -o create-chain .