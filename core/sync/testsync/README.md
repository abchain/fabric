This test project help to run a syncing progress as demo:

* Run the server node by setting `sync.test.server` to be true, for first time it will prepare the ledger. server print out the target height and hash for client, client should put it in `sync.test.targetblock`

* Run any client, which can be spawn with the same config file (core.yaml) and the run script (with an integer as the parameter).

* Client's datapath must be cleanned manually before running

About some issues:

+ Server fail when running with data built before:
    + Set `ledger.sanitycheck` entry in config file for a true value

+ Client can not sync world-state and indicate "no match"
    + When server is run with existed data, it has at most one state snapshot which can
      be provided for clients. So you must set the block count in server align to the
      "snapshot interval" (in default is 16) so server will cache current state
