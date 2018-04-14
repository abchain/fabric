#!/bin/bash

NUM_F=$1
TAG_CLEAR=$2
FABRIC_TOP=${GOPATH}/src/github.com/abchain/fabric
BUILD_BIN=${FABRIC_TOP}/build/bin
PEER_BINARY=embedded
CONSENSUS=pbft

if [ $# -eq 0 ]; then
    echo "runpbft.sh <f> <clear>"
    exit
fi

let NUM_N=$NUM_F*3+1


function runpeers {
    NUM_VP=$1
    NUM_LVP=$2
    NUM_NVP=$3
    CONSENSUS=$4
    CORE_PBFT_GENERAL_F=$5
    ACTION_CLEAR=$6
    export CORE_PEER_VALIDATOR_TRUSTEDVALIDATORS=$8

    TAG_CLEAR="clear"
    if [ $# -eq 0 ]; then
        echo "runpeers.sh <#vp> <#lvp> <#nvp> <pbft | noops> <f> <clear>"
        exit
    fi

    if [ $ACTION_CLEAR = "clearall" ];then
        echo 'a' > peer0.json
        echo 'a' > peer1.json
        echo 'a' > peer2.json
        echo 'a' > peer4.json
        rm -rf /var/hyperledger/*
    fi


    if [ $ACTION_CLEAR = "clearlog" ];then
        echo 'a' > peer0.json
        echo 'a' > peer1.json
        echo 'a' > peer2.json
        echo 'a' > peer4.json
    fi

    if [ $ACTION_CLEAR = "cleardb" ];then
        rm -rf /var/hyperledger/*
    fi

    buildpeerex

    if [ ! -f ${BUILD_BIN}/embedded ]; then
        echo 'No such a file: '${BUILD_BIN}'/embedded'
        exit -1
    fi

    killbyname peer_fabric_

    index=0
    let allpeer=$NUM_VP
    while [ $index -lt $allpeer ]; do
        startpeer vp $index $CONSENSUS $CORE_PBFT_GENERAL_F
        let index=index+1
    done

    allpeer=0
    let allpeer=index+$2
    while [  $index -lt $allpeer ]; do
        startpeer lvp $index $CONSENSUS $CORE_PBFT_GENERAL_F
        let index=index+1
    done

    let allpeer=index+$3
    while [  $index -lt $allpeer ]; do
        startpeer nvp $index $CONSENSUS $CORE_PBFT_GENERAL_F
        let index=index+1
    done

    ps -ef|grep "peer_fabric_"|grep -v grep |awk '{print "New processid: "$2 ", " $8}'
}

function buildpeerex {

    if [ ! -d ${BUILD_BIN} ]; then
        mkdir -p ${BUILD_BIN}
    fi

    if [ -f ${BUILD_BIN}/${PEER_BINARY} ]; then
        rm ${BUILD_BIN}/${PEER_BINARY}
    fi

    CGO_CFLAGS=" " CGO_LDFLAGS="-lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy" \
        GOBIN=${GOPATH}/src/github.com/abchain/fabric/build/bin go install github.com/abchain/fabric/examples/chaincode/go/embedded


    if [ ! -f ${BUILD_BIN}/${PEER_BINARY} ]; then
        echo 'Failed to build '${BUILD_BIN}/${PEER_BINARY}
        exit -1
    fi


    if [ ! -L ${BUILD_BIN}/peerex ]; then
        cd ${BUILD_BIN}
        ln -s ${PEER_BINARY} peerex
    fi
}


function killbyname {
    NAME=$1
    MYNAME="runpbft.sh"

    if [ $# -eq 0 ]; then
        echo "$MYNAME <process name>"
        exit
    fi

    ps -ef|grep "$NAME"|grep -v grep |grep -v $MYNAME |awk '{print "kill -9 "$2", "$8}'
    ps -ef|grep "$NAME"|grep -v grep |grep -v $MYNAME |awk '{print "kill -9 "$2}' | sh
    echo "All <$NAME*> killed!"
}

function startpeer {

    PEER_MODE=$1
    PEER_ID=$2
    CONSENSUS=$3
    NUM_F=$4
    TAG_LVP="lvp"
    TAG_PBFT="pbft"
    PORT_PREFIX=2
    let NUM_N=$NUM_F*3+1

    if [ $# -eq 0 ]; then
        echo "startpeer.sh <PEER_MODE> <#PEER_ID> <pbft | noops> <f>"
        exit
    fi

    echo "======================================================"

    FULL_PEER_ID=${PEER_MODE}${PEER_ID}

    if [ "$PEER_MODE" = "$TAG_LVP" ];then
        FULL_PEER_ID=${PEER_MODE}${PEER_ID}
    fi

    if [ $PEER_ID -gt 0 ];then
        export CORE_PEER_DISCOVERY_ROOTNODE=127.0.0.1:${PORT_PREFIX}055
        echo "The <$FULL_PEER_ID> started up with <$CONSENSUS> consensus. CORE_PEER_DISCOVERY_ROOTNODE=$CORE_PEER_DISCOVERY_ROOTNODE"
    else
        echo "The lead peer <$FULL_PEER_ID> started up with <$CONSENSUS> consensus"
    fi


    if [ "$CONSENSUS" = "$TAG_PBFT" ];then
        export CORE_PBFT_GENERAL_N=$NUM_N
        export CORE_PBFT_GENERAL_F=$NUM_F
        #echo "CORE_PBFT_GENERAL_N=$CORE_PBFT_GENERAL_N, CORE_PBFT_GENERAL_F=$CORE_PBFT_GENERAL_F"
    fi

    echo "======================================================"
    export CORE_PEER_VALIDATOR_CONSENSUS_PLUGIN=$CONSENSUS

    export CORE_LOGGING_OUTPUT_FILE=peer${PEER_ID}.json
    #export CORE_LOGGING_OUTPUTFILE=${FULL_PEER_ID}.json

    export CORE_CLI_ADDRESS=127.0.0.1:${PORT_PREFIX}${PEER_ID}52
    export CORE_REST_ADDRESS=127.0.0.1:${PORT_PREFIX}${PEER_ID}50

    export CORE_SERVICE_ADDRESS=127.0.0.1:${PORT_PREFIX}${PEER_ID}51
    export CORE_SERVICE_CLIADDRESS=127.0.0.1:${PORT_PREFIX}${PEER_ID}51

    export CORE_PEER_ID=${FULL_PEER_ID}
    export CORE_PEER_LOCALADDR=127.0.0.1:${PORT_PREFIX}${PEER_ID}56
    export CORE_PEER_LISTENADDRESS=127.0.0.1:${PORT_PREFIX}${PEER_ID}55
    export CORE_PEER_ADDRESS=127.0.0.1:${PORT_PREFIX}${PEER_ID}55
    export CORE_PEER_CLIADDRESS=127.0.0.1:${PORT_PREFIX}${PEER_ID}51

    export CORE_PEER_VALIDATOR_EVENTS_ADDRESS=0.0.0.0:${PORT_PREFIX}${PEER_ID}53
    export CORE_PEER_VALIDATOR_MODE=${PEER_MODE}

    export CORE_PEER_VALIDATOR_EVENTS_ADDRESS=0.0.0.0:${PORT_PREFIX}${PEER_ID}53
    export CORE_PEER_PKI_ECA_PADDR=localhost:${PORT_PREFIX}${PEER_ID}54
    export CORE_PEER_PKI_TCA_PADDR=localhost:${PORT_PREFIX}${PEER_ID}54
    export CORE_PEER_PKI_TLSCA_PADDR=localhost:${PORT_PREFIX}${PEER_ID}54
    export CORE_PEER_PROFILE_LISTENADDRESS=0.0.0.0:${PORT_PREFIX}${PEER_ID}60

    export CORE_PEER_FILESYSTEMPATH=/var/hyperledger/production${PEER_ID}

    if [ ! -f ${BUILD_BIN}/peer_fabric_${PEER_ID} ]; then
        cd ${BUILD_BIN}
        ln -s peerex peer_fabric_${PEER_ID}
        cd ../..
    fi

    if [ ! -d ${FABRIC_TOP}/stderrdir ]; then
        mkdir ${FABRIC_TOP}/stderrdir
    fi

    # build/bin/peer_fabric_${PEER_ID} node start
    nohup ${BUILD_BIN}/peer_fabric_${PEER_ID} node start > /dev/null 2>${FABRIC_TOP}/stderrdir/err_nohup_peer${PEER_ID}.json &
    #nohup ${BUILD_BIN}/peer_fabric_${PEER_ID} node start > ${FABRIC_TOP}/nohup_peer${PEER_ID}.log 2>&1 &
}


function main {
    runpeers $NUM_N 0 0 $CONSENSUS $NUM_F $TAG_CLEAR
}




medias=(x c v)

function count {

for m in ${medias[*]}; do
    echo "$m article :"
 done

}

medias=(x c v)

function count2 {

for i in 1 5 10 20 50 100 200 400 600 800 1000 2000 5000 10000 20000 50000 100000; do
    echo "$i element article :"
done

}

for media in ${medias[@]}; do
        echo "hh ${media}"
done

count2
#main