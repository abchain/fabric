#!/usr/bin/env bash
set -e

main() {

    for ((index=0; index<$1; index++)) do
        res=`./5nodes_test.sh |grep "Result: 20"| wc -l`

        if [ $res -lt 5 ] && [ $res -gt 0 ]; then
            echo "unexpected: $res"
            exit
        fi

        echo $res
    done
}

main $1
