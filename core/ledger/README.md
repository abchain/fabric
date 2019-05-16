## Ledger Package

This package implements the ledger, which includes the blockchain and global state.

YA-fabric has refactored this module and provided a more robust implement, with entries easy to be used

### Mutiple ledgers

In YA-fabric, developer can run mutiple ledger objects in an instance. All of them have their separated
blockchain, state and block indexes. And they share a storage pool for graphics of states, and transactions,
which can be accessed from the globalLedger singleton (Each ledger object also integrated the entry of 
globalLedger)

`GetNewLedger` API can be used to create new ledger, replace of the legacy "GetLedger". The later function
can still be used for accessing a default ledger object

### Concurrent supporting

A ledger object now can be read by mutiple-thread and written by single-thread (Single Write Mutiple Reads,
or SWMR). User can use a snapshot of database to get data consistent in a query transction, even another
transaction is just changing the underlying database

### Execution and committing entries

`TxExecState`, a new designed object, is provided for executing transactions, replace the old API for world-states.
The new object allow concurrent execution of txs, and in combination of the committing entries form a new
environment for chaincode platform.

A series of committing objects, see `ledger_commit.go` and `ledger_commit_sync.go` is used for updating
ledger now. They have a unified interface on handling each elements in the ledger (blocks, states and indexes) 
and can be applied by requirement of different scenes like committing a new block after executing a bunch
of transactions, inserting a block, or syncing the whole world-state, etc.

### Syncing of world-state

A practical syncing implement for the whole world-state, via the new `Dividable` interface, is avaliable now.
So peers can transfer their world-state data with resumable, verifiable process, which may be able to save
much traffic and computational cost.

*Currently only buckettree has implied the 'diviable' interface*
