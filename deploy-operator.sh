#!/bin/sh

. ./env.sh

oc apply -f ./deploy/role.yaml -n ${PROJECT_NAME}
oc apply -f ./deploy/service_account.yaml -n ${PROJECT_NAME}
oc apply -f ./deploy/role_binding.yaml -n ${PROJECT_NAME}

oc apply -f ./deploy/crds/kharon_v1alpha1_canary_crd.yaml -n ${PROJECT_NAME}

cat ./deploy/operator.yaml | \
  sed "s/{{\b*QUAY_USERNAME\b*}}/${QUAY_USERNAME}/" | \
  sed "s/{{\b*OPERATOR_VERSION\b*}}/${OPERATOR_VERSION}/" | oc apply -n ${PROJECT_NAME} -f -
