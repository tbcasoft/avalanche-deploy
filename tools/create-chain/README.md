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

## Usage
export PLATFORM_CLI_KEY_PASSWORD=[WALLET_PASSWORD]

Note:  by default, wallet stored in platform-cli keystore.  This wallet is password protected.

ubuntu@ip-10-8-3-214: /tbcasoft/avalanche-deploy-with-my-notes/tools/create-chain$ ./create-chain -network=fuji \ 
-key-name=my-l1-key-admin-created-from-platform-cli \
-subnetidstr="2MGNFLqeLP9EgHvLT2hfdKSvkLFwnGa1o7cDXPPcQQ88CB9C1v" \
-chain-name="vdnL1a" \ 
-genesis-file="/home/ubuntu/tbcasoft/Determined-Achiever-Chain-metadata/genesis.json"
