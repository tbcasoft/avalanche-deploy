# Convert subnet to L1

CLI tool to convert a subnet to an L1 on Fuji or Mainnet.

## Prerequisites

1. **Funded P-Chain Address**: You need a P-Chain address with AVAX
    - This tool assummed wallet is stored in platform-cli keystore.
2. **Existence of subnet+**
3. **Existence of chain**
4. **Active validators**

## Functionality
See e2e-l1.sh for the flow.  The important note is the flow is dependent on the arguments passed.  If not passed correctly, the deployment flow iw wrong.

## Step 1
1. **Create Subnet**
2. **Create Chain+**
3. **Create Validator(s)**
4. **You need to manually launch and configure each validator to track subnet and validate blockchain**
5. **Convert subnet to L1**

Note:  by default, wallet stored in platform-cli keystore.  This wallet is password protected.

Note:  xfacade0000000000000000000000000000000000 is out-of-box Validator Manager Proxy.  It will be use as part of conversion.

Note:  rpcurl must use the P-Chain endpoint for the network where this subnet was created—not the new L1’s /ext/bc/<blockchainID>/rpc endpoint.

export PLATFORM_CLI_KEY_PASSWORD=[YOUR_PASSWORD]
ubuntu@ip-10-8-3-214:/data/tbcasoft/avalanche-deploy/tools/convert-subnet-to-l1$ ./convert-subnet-to-l1 \
--network="fuji" \
--subnetidstr=[E.G. 2MGNFLqeLP9EgHvLT2hfdKSvkLFwnGa1o7cDXPPcQQ88CB9C1v] \
--chainidstr="2VsBtoGoe5fz6kG78TMYGPgMFhsk6e7jbktsDpvkSJzVDSgZYR" \
--key-name="my-l1-key-admin-created-from-platform-cli" \
--rpcurl="http://18.219.181.165:9650" \
--validators=[host:9650.  Format is host:port.  The port is NOT the stakingn port (9651), use the api / http port 9650.  And don't include the protocol http and https] \
--genesis-proxy-address="0xfacade0000000000000000000000000000000000" \
--contracts-path="/home/ubuntu/tbcasoft/icm-services" \
--output-file="convert-subnet-to-l1"

## Prerequisites
The new L1 is the destination of smart contract(s) deployment.  In addition, deployment is a transaction on the blockchain which would result
in a new block on the chain.  Hence, conversion of subnet has completed and validator nodes are syned and producing blocks.  

## Step 2
1. **Execute tools/initialize-validator-manager/initialize-validator-manager**
2. **xfacade0000000000000000000000000000000000 is out-of-box Validator Manager Proxy.**
3. **max-churn-percent** set this to 50 (50%) or 100 (100%) at launch. Setting it to 50% allows you to add a 3rd equal-weight validator immediately without hitting the cap. Setting it to 100% completely removes churn friction while you are bootstrapping your initial cluster.
4. **churn-period** period you need to wait before adding validator if you hit the max churn percentage.  Recommend 3600 (1 hour) as you only have to wait 60 minutes before adding the next one.

export PLATFORM_CLI_KEY_PASSWORD=[YOUR_PASSWORD]

ubuntu@ip-10-8-3-214:/data/tbcasoft/avalanche-deploy/tools/initialize-validator-manager/initialize-validator-manager$ ./initialize-validator-manager \
--rpc-url="http://RPC URL containing the blockchain ID of the L1, point it to one of your validators:9650" \
--proxy-address="xfacade0000000000000000000000000000000000" \
--subnet-id={{ subnet_id }} \
--chain-id={{ chain_id }} \
--conversion-tx={{ conversion_tx }} \
--manager-type={{ manager_type }} \
--network={{ network }} \
--validator-ips={{ validator_ips }} \
--churn-period=100 \
--max-churn-percent={{ max_churn_percent }} \
--contracts-path="/home/ubuntu/tbcasoft/icm-services" \
--local-sig-agg=true \
--json
