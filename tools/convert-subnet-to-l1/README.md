# Convert subnet to L1

CLI tool to convert a subnet to an L1 on Fuji or Mainnet.

## Prerequisites

1. **Funded P-Chain Address**: You need a P-Chain address with AVAX
    - This tool assummed wallet is stored in platform-cli keystore.
2. **Existence of subnet+**
3. **Existence of chain**
4. **Active validators**
5. **Forge**: Used to deploy smart contract onto your chain

## Functionality
1. **Convert a subnet to L1
2. **Deploy Validator Manager, requires Forge
3. **Deploy Validator Manager Proxy using the provided address

## Usage
export PLATFORM_CLI_KEY_PASSWORD=[YOUR_PASSWORD]

Note:  by default, wallet stored in platform-cli keystore.  This wallet is password protected.

ubuntu@ip-10-8-3-214:/data/tbcasoft/avalanche-deploy/tools/convert-subnet-to-l1$ ./convert-subnet-to-l1 \
--subnetidstr=[E.G. 2MGNFLqeLP9EgHvLT2hfdKSvkLFwnGa1o7cDXPPcQQ88CB9C1v] \
-key-name my-l1-key-admin-created-from-platform-cli \
--validators=[Comma-separated validators.  Format is host:port, don't include the protocol http and https] \
--genesis-proxy-address="0xfacade0000000000000000000000000000000000" \
--deploy-validator-manager=true