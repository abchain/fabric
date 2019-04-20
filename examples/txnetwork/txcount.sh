#!/usr/bin/env bash
#set -e

main() {

    for ((index=0; index<$1; index++)) do
        res=`./t5.sh |grep "Result: 20"| wc -l`

        if [ $res -lt 5 ] && [ $res -gt 0 ]; then
            echo "unexpected: $res"
            exit
        fi

        echo $res
#        sleep 3
    done
}

main $1
