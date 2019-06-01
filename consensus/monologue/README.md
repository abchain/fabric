## MONOGUE Consensus 

MONOGUE is the simplest consensus implement, like the "noops" in fabric-0.6. It suppose only one node is selected for determinding a single block, and any other nodes join in this consensus will admit it.

Mutiple nodes can appoint one of them for responding one blocks via some external consensus progress (such as an election in etcd), to avoid single-point failure. So there may be mutiple candidates for a single block but before any next candidate is delivered, every node must admit one of them, and the hash of the admited block is just the previous hash field in all of the next candidates. As the result, there will be always one-block lag for each transaction being added onto the chain