#!/usr/bin/env bash

PEERLOCALADDRBASE=7055

PEER_BINARY=../../../peer/peer

function invokebody {

    ((LOCADDRPORT = PEERLOCALADDRBASE + $1 * 100))
    export CORE_SERVICE_CLIADDRESS=127.0.0.1:${LOCADDRPORT}
    ${PEER_BINARY} chaincode invoke -n txnetwork -c "{\"Function\": \"invoke\", \"Args\": [\"aa\",\"$1\"]}"
}

invokebody $1




