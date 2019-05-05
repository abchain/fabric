#!/usr/bin/env bash
#set -e

main() {

    num_peer=$1
    ((num_connection=num_peer * num_peer - num_peer))
    for ((index=0; index<${2}; index++)) do
        res=`./run.sh ${num_peer} |grep "ESTABLISHED"| wc -l`

        if [ $res -lt ${num_connection} ] && [ $res -gt 0 ]; then
            echo "unexpected: $res"
            exit
        fi

        echo $res
    done
}

main $1 $2
