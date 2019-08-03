## YA-fabric

YA-fabric is an independent project branched from the hyperledger fabric 0.6. It purposed to be another future of its parent, mostly keeping the original architecture and continue it under the abchain plan and developments.

What we have achieved include:

* A blockchain framework ready for production, including security and applicating consideration

* A network focus on dispatching and syncing transactions on Scuttlebutt model (one of the Gossip Protocol), under a throughput of several thousands TPS

* Supporting hyperledger's chaincode by a stable platform, with the ability of concurrent execution of transactions

* An improved architecture of distributed ledger for tracing and working on mutiple global states. For more idea and thinking about the designation of ledger, read our article about a "[consensus framework]()" (in Chinese)

* User can build consensus modules base on a chaincode, and made use of every part of the framework to serve to their business logic

* Easy to be packed one or more chaincode with the framework and distrubute it as a standalone blockchain program, i.e. integration chaincodes into the fabric peer

* Compatible to the old fabric 0.6 project: the chaincode, data and configurations will be respected and can continue working on YA-fabric

## Our plans (after 0.9)

* Immigrating PBFT module into the consensus framework, with dynamic decision of the involving peers

* Implement of ledger rolling back, current code has been well prepared but we still need some lines to make it work (also the testing)

* Concurrent execution of transactions

* API help to evaluate the cost of tx executions

If you wish to take a look at the original readme of fabric 0.6, found [here](https://github.com/hyperledger/fabric/blob/v0.6/README.md)

## Installation

### Rocksdb

* Use rocksdb 5.17

### Dependencies

* Now we are making an effort to discard the outdate dependencies in our project and rebuild it base on modern dep. management scheme. 

  We are underway and only part of the dependencies have been upgrade. A go dep scheme is provided and if you wish to apply the project under go mod,
  remember keep vendored projected being involved in building

  To manually resolve the dependencies, following libraries should be install:

  + protobuf
  + grpc
  + golang.org/x/net,crypto,sys,text

## Examples

  build github.com/abchain/fabric/examples/chaincode/embedded, experience the power of embedded chaincode

## Release 0.97

* Upgrade dependencies

## Release 0.96

This is the first stable version which can be applied under production environment

* A fully implement for credential, base on x509 certificates

* Many bug fixing

## Release a preview of 0.95

* The backbone of consensus framework

* Syncing module 

* A new ledger package, see the [readme](https://github.com/abchain/fabric/blob/0.9/core/ledger/README.md)

## Release of 0.9 is skipped for the reversed developing plan and we haved worked for a further version

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


## License <a name="license"></a>
The Abchain Project uses the [Apache License Version 2.0](LICENSE) software
license.
