#!/usr/bin/env bash
#set -e

main() {

    for ((index=0; index<$2; index++)) do
        res=`./run.sh $1 |grep "ESTABLISHED"| wc -l`

        if [ $res -lt 12 ] && [ $res -gt 0 ]; then
            echo "unexpected: $res"
            exit
        fi

        echo $res
    done
}

main $1 $2
