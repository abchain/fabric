#!/usr/bin/env bash
set -e

PEERADDRBASE=7055
EVENTADDRBASE=7053
FILEPATHBASE=/var/hyperledger
PEER_BINARY=../../peer/peer

BRIDGE_URL=127.0.0.1:7055
ROOTNODE1_URL=127.0.0.1:7155
ROOTNODE2_URL=127.0.0.1:7255

#* bridge(discovery_disable=true, rootnode=nsa,nsb)
#* nsa(discovery_hidden=true)
#* nsb(discovery_hidden=true)
#* a1(rootnode=nsa)
#* a2(rootnode=nsa)
#* b1(rootnode=nsb)
#* b2(rootnode=nsb)
PEER_LIST=(bridge nsa nsb a1 a2 b1 b2)
MINER_LIST=(3 4 5 6)

function startpeer {

    index=$1
    name=$2
    rootnode=$3
    discovery_hidden=$4
    discovery_disable=$5

    ((ADDRPORT = PEERADDRBASE + index * 100))
    ((EVENTADDRPORT = EVENTADDRBASE + index * 100))
    export CORE_PEER_LISTENADDRESS=127.0.0.1:${ADDRPORT}
    export CORE_PEER_ADDRESS=${CORE_PEER_LISTENADDRESS}
    export CORE_PEER_ID=${index}_${name}
    export CORE_PEER_VALIDATOR_EVENTS_ADDRESS=127.0.0.1:${EVENTADDRPORT}
    export CORE_PEER_FILESYSTEMPATH=${FILEPATHBASE}/txnet${index}
    export CORE_PEER_DISCOVERY_ROOTNODE=${rootnode}
    export CORE_PEER_DISCOVERY_HIDDEN=${discovery_hidden}
    export CORE_PEER_DISCOVERY_DISABLE=${discovery_disable}
    export CORE_LOGGING_NODE=info:peer=debug

#	discoveryHidden = viper.GetBool("peer.discovery.hidden")
#	discoveryDisable = viper.GetBool("peer.discovery.disable")
    export LOG_STDOUT_FILE=_stdout_txnetwork_$1_$2.json
    echo Run node ${CORE_PEER_ADDRESS} ...
    ln -snf txnetwork txnetwork_${CORE_PEER_ID}
#    sleep 2

    ./txnetwork_${CORE_PEER_ID} >> ${LOG_STDOUT_FILE} 2>>${LOG_STDOUT_FILE} &
}


function start {
    go build
    ./killbyname.sh txnetwork
    rm ./txnetwork_*
    rm -rf ${FILEPATHBASE}/txnet*
    rm *_stdout_*_*.json
    startpeer 0 bridge ${ROOTNODE1_URL},${ROOTNODE2_URL} false true
    startpeer 1 nsa "" true false
    startpeer 2 nsb "" true false
    startpeer 3 a1 ${ROOTNODE1_URL} false false
    startpeer 4 a2 ${ROOTNODE1_URL} false false
    startpeer 5 b1 ${ROOTNODE2_URL} false false
    startpeer 6 b2 ${ROOTNODE2_URL} false false
}

tx_count() {
    ${PEER_BINARY} chaincode query -n txnetwork -c "{\"Function\": \"count\", \"Args\": []}"
}

network_list() {
    ${PEER_BINARY} network list
}


query() {
    action=${2}
    printf "=================================================\n"
    printf "==============${action}=================\n\n"

    for ((index=0; index<$1; index++)) do
        ((LOCADDRPORT = PEERADDRBASE + ${index} * 100))
        export CORE_SERVICE_CLIADDRESS=127.0.0.1:${LOCADDRPORT}
        printf "Dump <${PEER_LIST[${index}]}> node ${action}\n"
        ${action}
        printf "\n"
    done
}

invoke() {
    printf "=================================================\n"
    printf "====================invoke=======================\n"

    for minerId in ${MINER_LIST[@]}
    do
        for ((index=0; index<$1; index++)) do
            ((LOCADDRPORT = PEERADDRBASE + ${minerId} * 100))
            export CORE_SERVICE_CLIADDRESS=127.0.0.1:${LOCADDRPORT}
            nodeName=${PEER_LIST[${minerId}]}

            printf "Send tx to <${nodeName}>: ${PEER_BINARY} chaincode invoke -n txnetwork -c \"{\"Function\": \"invoke\", \"Args\": [\"key${nodeName}_${index}\",\"${index}\"]}\"\n"
            ${PEER_BINARY} chaincode invoke -n txnetwork \
                -c "{\"Function\": \"invoke\", \"Args\": [\"key${nodeName}${index}\",\"${index}\"]}"
        done
    done
}

main() {

#       for ((index=0; index<${#MINER_LIST[*]}; index++)) do
#          echo "${MINER_LIST[${index}]}"
#       done

    start
    sleep 2
    query 7 network_list
#    invoke 7
#    sleep 5
#    query 7 tx_count
}

main
