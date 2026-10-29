# create-l1 Creates a Avalanche subnet.

## Prerequisites
1. **Funded P-Chain Wallet**: You need a P-Chain address with AVAX
    - Fuji: Get test AVAX from [faucet.avax.network](https://faucet.avax.network/)
    - Mainnet: Real AVAX required

## Build

```bash
go build -o create-subnet .
```

## Usage
export PLATFORM_CLI_KEY_PASSWORD=[YOUR_PASSWORD]

Note:  by default, wallet stored in platform-cli keystore.  This wallet is password protected.

ubuntu@ip-10-8-3-214: /tbcasoft/avalanche-deploy-with-my-notes/tools/create-subnet$ ./create-subnet -network=fuji -key-name=my-l1-key-admin-created-from-platform-cli -rpcurl=http://18.219.181.165:9650