#!/bin/sh

. ./env.sh

git commit -a

git tag -a v${OPERATOR_VERSION} -m "Releasing version v${OPERATOR_VERSION}"

git push origin v${OPERATOR_VERSION} ; git push kharon v${OPERATOR_VERSION}