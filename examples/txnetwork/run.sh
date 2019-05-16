#!/usr/bin/env bash

PEERADDRBASE=7055
EVENTADDRBASE=7053
FILEPATHBASE=/var/hyperledger

function startpeer {

    index=$1
    ((ADDRPORT = PEERADDRBASE + index * 100))
    ((EVENTADDRPORT = EVENTADDRBASE + index * 100))

    export CORE_PEER_LISTENADDRESS=127.0.0.1:${ADDRPORT}
    export CORE_PEER_ADDRESS=${CORE_PEER_LISTENADDRESS}
    export CORE_PEER_ID=billgates_${index}
    export CORE_PEER_VALIDATOR_EVENTS_ADDRESS=127.0.0.1:${EVENTADDRPORT}
    export CORE_PEER_FILESYSTEMPATH=${FILEPATHBASE}/txnet${index}

    export LOG_STDOUT_FILE=_stdout_${CORE_PEER_ID}.json
    echo Run node ${CORE_PEER_ADDRESS} ...
    ./txnetwork >> ${LOG_STDOUT_FILE} 2>>${LOG_STDOUT_FILE} &
    #sleep 1
}


function main {
    go build
    ./killbyname.sh txnetwork
    rm *_stdout_billgates_*.json
    for ((index=0; index<$1; index++))
    do
        startpeer ${index}
    done
}

main $1
