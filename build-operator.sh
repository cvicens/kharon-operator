#!/bin/sh

. ./env.sh

operator-sdk build quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:${OPERATOR_VERSION}