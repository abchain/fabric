#!/bin/bash

EXECUTION=dbscan

./killbyname.sh peer_fabric


rm ./${EXECUTION}

go build
./${EXECUTION} -dbpath /var/hyperledger/production$1 -mode q

ls -l /var/hyperledger/production$1/checkpoint/db
