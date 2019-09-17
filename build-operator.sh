#!/bin/sh

. ./env.sh

operator-sdk build quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:v${OPERATOR_VERSION}