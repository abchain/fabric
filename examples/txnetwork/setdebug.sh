#!/usr/bin/env bash

PEERLOCALADDRBASE=7055

PEER_BINARY=../../peer/peer


function query {

    ((LOCADDRPORT = PEERLOCALADDRBASE + $1 * 100))
    export CORE_SERVICE_CLIADDRESS=127.0.0.1:${LOCADDRPORT}

#    ${PEER_BINARY} network status
    ${PEER_BINARY} chaincode query -n txnetwork -c "{\"Function\": \"debug\", \"Args\": [\"on\"]}"
#    ${PEER_BINARY} chaincode query -n txnetwork -c "{\"Function\": \"status\", \"Args\": []}"
}

query $1