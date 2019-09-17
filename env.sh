#!/bin/sh

# oc
export PATH=~/Projects/openshift/operators/bin:$PATH

# SDK VERSION
export RELEASE_VERSION=v0.10.0

# API VERSION AND OPERATOR NAME
export API_VERSION="kharon.redhat.com/v1alpha1"
export OPERATOR_NAME="kharon-operator"

# GO
export GO111MODULE=on

# Operator
export OPERATOR_VERSION=0.0.4
export PROJECT_NAME=${OPERATOR_NAME}-tests

# Quay
export QUAY_USERNAME=cvicensa