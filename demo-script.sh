#!/bin/sh

. ./env.sh

oc new-project ${PROJECT_NAME}

for i in {0..2};
do
    oc tag quay.io/${QUAY_USERNAME}/kharon-test:v1.${i}.0 kharon-operator-tests/kharon-test:v1.${i}.0 --scheduled=true -n ${PROJECT_NAME}
done

sleep 10

for i in {0..2};
do
    oc new-app kharon-operator-tests/kharon-test:v1.${i}.0 --name kharon-test-v1-${i}-0 -n ${PROJECT_NAME}
done

oc apply -f ./deploy/role.yaml
oc apply -f ./deploy/service_account.yaml
oc apply -f ./deploy/role_binding.yaml
oc apply -f ./deploy/crds/kharon_v1alpha1_canary_crd.yaml

echo "RUN: oc apply -f ./deploy/crds/kharon_v1alpha1_canary_crd.yaml and modify target to point to kharon-test-v1-0-0, kharon-test-v1-1-0 and kharon-test-v1-2-0"
echo "When moving to a new version have a look to routes: oc get route -n ${PROJECT_NAME}"
echo "Also run: oc get route -n ${PROJECT_NAME}"