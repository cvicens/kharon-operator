#!/bin/sh

. ./env.sh

VERSION=$1

HOST=$(oc get route kharon-test-v1-${VERSION}-0 -o json -n $PROJECT_NAME | jq -r '.spec.host')

for i in `seq 1 20000`; 
do 
sleep 2
curl -ks http://${HOST}/api/greeting?error=xyz > /dev/null
printf "x"
done