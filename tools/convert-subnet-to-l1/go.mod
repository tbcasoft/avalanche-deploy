module github.com/tbcasoft/avalanche-deploy/tools/convert-subnet-to-l1

go 1.26.4

replace github.com/tbcasoft/avalanche-deploy/tools/core => ../core
require (
	github.com/ava-labs/avalanchego v1.15.0
	github.com/tbcasoft/avalanche-deploy/tools/core v0.0.0-20261029184725-54fe1a2d0f22
)