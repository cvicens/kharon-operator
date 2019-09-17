#!/bin/sh

. ./env.sh

git commit -a

git tag -a ${OPERATOR_VERSION} -m "Releasing version ${OPERATOR_VERSION}"

git push origin ${OPERATOR_VERSION} ; git push kharon ${OPERATOR_VERSION}