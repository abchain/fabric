## YA-fabric

This is purposed to be another future of fabric 0.6, keeping the original architecture and continue it with our improvments.

The original readme can be found [here](https://github.com/hyperledger/fabric/blob/v0.6/README.md)

## Installation

### Rocksdb

* Use rocksdb 5.10

## Release of 0.8

* Refactoring ledger package, towards a novel framework of consensus

    **User upgraded from 0.7 or older legacy version should be install dbupgrade utility and run first**

* Stream mutiplexing on p2p communication instead of the sole chat stream

* New mode for reusing transaction certification

    **Now a Peer of 0.8 could not work with peers built by old version**

* Gossip-base transaction flows (alpha version)

* Lots of code refoctoring

## Release of 0.7

* Many bug fixes since fabric 0.6, ready to production

* Membersrvc is dynamically configurable

* "Embedded" mode enable developer to bundle their chaincode with fabric peer

* More roles in p2p network are induced

* Core depedencies to vendor packages (protobuf, grpc, rocksdb) are up-to-date now

## Our plans

* A "real" impelemnt for production, including some security and applicating consideration

* Completing some features and making improvments for the chaincode part

* Easy to be packed as a "standalone" blockchain, that is, the integration of chaincodes and fabric peer

* Our implement for the concurreny handling of transactions

* More consensus modules


## License <a name="license"></a>
The Abchain Project uses the [Apache License Version 2.0](LICENSE) software
license.
