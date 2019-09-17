#!/bin/sh

. ./env.sh

# Create a project for our demo
oc new-project ${PROJECT_NAME}

# Tag images in OCP so that they're refreshed from quay
for i in {0..2};
do
    oc tag quay.io/${QUAY_USERNAME}/kharon-test:v1.${i}.0 kharon-operator-tests/kharon-test:v1.${i}.0 --scheduled=true -n ${PROJECT_NAME}
done

sleep 10

# Deploy the test app (versions 0, 1 and 2)
for i in {0..2};
do
    oc new-app kharon-operator-tests/kharon-test:v1.${i}.0 --name kharon-test-v1-${i}-0 -n ${PROJECT_NAME}
    oc expose svc/kharon-test-v1-${i}-0 -n ${PROJECT_NAME}
    oc label svc/kharon-test-v1-${i}-0 team=spring-boot-actuator -n ${PROJECT_NAME}
done

# Deploy the operator itself
./deploy-operator.sh

echo "RUN: oc apply -f ./deploy/crds/kharon_v1alpha1_canary_crd.yaml and modify target to point to kharon-test-v1-0-0, kharon-test-v1-1-0 and kharon-test-v1-2-0"
echo "When moving to a new version have a look to routes: oc get route -n ${PROJECT_NAME}"
echo "Also run: oc get route -n ${PROJECT_NAME}"