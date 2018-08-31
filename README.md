Bytom SPV Wallet
====

[![Build Status](https://travis-ci.org/Bytom/bytom.svg)](https://travis-ci.org/Bytom/bytom) [![AGPL v3](https://img.shields.io/badge/license-AGPL%20v3-brightgreen.svg)](./LICENSE)

**Official golang implementation of the Bytom SPV Wallet.**

Automated builds are available for stable releases and the unstable master branch. Binary archives are published at https://github.com/bytom-spv/bytom-spv/releases.

## What is Bytom SPV Wallet?
SPV wallet verifies that a transaction is included in the  Bytom blockchain, without downloading the entire blockchain. The SPV wallet only needs to download the block headers, which are much smaller than the full blocks. To verify that a transaction is in a block, SPV wallet requests a proof of inclusion, in the form of a Merkle branch.


In the current state `bytom spv wallet` is able to:

- Manage key, account as well as asset
- Send transactions, i.e., issue, spend and retire asset


## Building from source

### Requirements

- [Go](https://golang.org/doc/install) version 1.8 or higher, with `$GOPATH` set to your preferred directory

### Installation

Ensure Go with the supported version is installed properly:

```bash
$ go version
$ go env GOROOT GOPATH
```

- Get the source code

``` bash
$ git clone https://github.com/bytom-spv/bytom-spv.git $GOPATH/src/github.com/bytom-spv
```

- Build source code

``` bash
$ cd $GOPATH/src/github.com/bytom-spv
$ make bytom-spv    # build bytom-spv-wallet
```

When successfully building the project, the `bytom-spv-wallet`  should be present in `cmd/bytomd` directory.

## Running bytom spv wallet

### Initialize

First of all, initialize the node:

```bash
$ cd ./cmd/bytomd
$ ./bytom-spv-wallet init --chain_id testnet -r ~/.bytom_spv
```

There are three options for the flag `--chain_id`:

- `mainnet`: connect to the mainnet.
- `testnet`: connect to the testnet wisdom.
- `solonet`: standalone mode.

After that, you'll see `config.toml` generated, then launch the node.

### launch

``` bash
$ ./bytom-spv-wallet node -r ~/.bytom_spv
```

### Dashboard

Access the dashboard:

```
$ open http://localhost:9888/
```

## Contributing

Thank you for considering helping out with the source code! Any contributions are highly appreciated, and we are grateful for even the smallest of fixes!

If you run into an issue, feel free to [bytom issues](https://github.com/bytom-spv/bytom/issues/) in this repository. We are glad to help!

## License

[AGPL v3](./LICENSE)

