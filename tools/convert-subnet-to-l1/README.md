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
--network="fuji" \
--subnetidstr=[E.G. 2MGNFLqeLP9EgHvLT2hfdKSvkLFwnGa1o7cDXPPcQQ88CB9C1v] \
--chainidstr="2VsBtoGoe5fz6kG78TMYGPgMFhsk6e7jbktsDpvkSJzVDSgZYR" \
--key-name="my-l1-key-admin-created-from-platform-cli" \
--rpcurl=http://18.219.181.165:9650 \
--validators=[host:9650.  Format is host:port.  The port is NOT the stakingn port (9651), use the api / http port 9650.  And don't include the protocol http and https] \
--genesis-proxy-address="0xfacade0000000000000000000000000000000000" \
--deploy-validator-manager=true \
--local-sig-agg=true \
--sig-agg-url="http://18.227.100.19:8090" \
--contracts-path="/home/ubuntu/tbcasoft/icm-services" \
--output-file="convert-subnet-to-l1"