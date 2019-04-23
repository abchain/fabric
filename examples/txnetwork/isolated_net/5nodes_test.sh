#!/usr/bin/env bash
set -e
PEERADDRBASE=7055
EVENTADDRBASE=7053
FILEPATHBASE=/var/hyperledger
PEER_BINARY=../../../peer/peer
BRIDGE_URL=127.0.0.1:7055
ROOTNODE_a_URL=127.0.0.1:7155
ROOTNODE_b_URL=127.0.0.1:7355
#* bridge(discovery_disable=true, rootnode=a1,b1)
#* a2(rootnode=a1)
#* b2(rootnode=b1)
PEER_LIST=(bridge a1 a2 b1 b2)
MINER_LIST=(1 2 3 4)

START_ONE="N"
REBUILD="N"

while getopts "bn:i:" opt; do
  case $opt in
    b)
      echo "REBUILD = Y"
      REBUILD="Y"
      ;;
    n)
      echo "NETWORK_ID = $OPTARG"
      NETWORK_ID=$OPTARG
      START_ONE="Y"
      ;;
    i)
      echo "NODE_ID = $OPTARG"
      NODE_ID=$OPTARG
      START_ONE="Y"
      ;;
    \?)
      echo "Invalid option: -$OPTARG"
      ;;
  esac
done


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
    ./txnetwork_${CORE_PEER_ID} >> ${LOG_STDOUT_FILE} 2>>${LOG_STDOUT_FILE} &
}

init() {

    if [ "${REBUILD}" = "Y" ]; then
        go build
    fi

    ../killbyname.sh txnetwork
    rm ./txnetwork_*
    rm -rf ${FILEPATHBASE}/txnet*
    rm *_stdout_*_*.json

}


function start_one {
    rootnode=${ROOTNODE_a_URL}
    networkId=$2
    nodeId=$1

    if [ "${networkId}" = "b" ]; then
        rootnode=${ROOTNODE_b_URL}
        echo "root = ROOTNODE_b_URL"
    fi

    startpeer ${nodeId} ${networkId}${nodeId} ${rootnode} false false
}

function start {
    startpeer 0 brigde ${ROOTNODE_a_URL},${ROOTNODE_b_URL} false true
    startpeer 1 a1 "" false false
    start_one 2 a

    startpeer 3 b1 "" false false
    start_one 4 b
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

invokebody() {

    ((LOCADDRPORT = PEERLOCALADDRBASE + $1 * 100))
    export CORE_SERVICE_CLIADDRESS=127.0.0.1:${LOCADDRPORT}
    ${PEER_BINARY} chaincode invoke -n txnetwork -c "{\"Function\": \"invoke\", \"Args\": [\"aa\",\"$1\"]}"
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

start_all() {
    init
    start
    sleep 5
    query 5 network_list
    invoke 5
    sleep 5
    query 5 tx_count

    /querytcpbyport.sh txnet
}


main() {
    if [ "$START_ONE" = "N" ]; then
        start_all
    else
        start_one $NODE_ID ${NETWORK_ID}
    fi
}

main