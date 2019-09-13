#!/bin/sh

. ./env.sh

declare -A HOSTS
for i in {0..2};
do
    HOSTS[${i}]=$(oc get route kharon-test-v1-${i}-0 -o json -n $PROJECT_NAME | jq -r '.spec.host')
done

for i in `seq 1 20000`; 
do 
    for i in {0..2};
    do
    sleep 0.25
    curl -ks http://${HOSTS[i]}/api/greeting > /dev/null
    printf "."
    done
done