#!/bin/sh

. ./env.sh

docker push quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:v${OPERATOR_VERSION}