#!/usr/bin/env bash
set -e
PEERADDRBASE=7055
EVENTADDRBASE=7053
FILEPATHBASE=/var/hyperledger
PEER_BINARY=../../peer/peer

tx_count() {
    ${PEER_BINARY} chaincode query -n txnetwork -c "{\"Function\": \"count\", \"Args\": []}"
}

network_list() {
    ${PEER_BINARY} network list
}

network_status() {
    ${PEER_BINARY} network status
}

txnetwork_status() {
    ${PEER_BINARY} chaincode query -n txnetwork -c "{\"Function\": \"status\", \"Args\": []}"
}


query() {
    action=${2}
    printf "=================================================\n"
    printf "==============${action}=================\n\n"

    for ((index=0; index<$1; index++)) do
        ((LOCADDRPORT = PEERADDRBASE + ${index} * 100))
        export CORE_SERVICE_CLIADDRESS=127.0.0.1:${LOCADDRPORT}
        printf "Dump <peer[${index}]> node ${action}\n"
        ${action}
        printf "\n"
    done
}


main() {
    lsof -i -n -P|grep txnetwork
    query $1 network_list
    query $1 txnetwork_status
    query $1 network_status
    query $1 tx_count
}

main $1
