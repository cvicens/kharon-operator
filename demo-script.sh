oc tag quay.io/cvicensa/kharon-test:v1.0.0 kharon-operator-tests/kharon-test:v1.0.0 --scheduled=true -n kharon-operator-tests
oc tag quay.io/cvicensa/kharon-test:v1.1.0 kharon-operator-tests/kharon-test:v1.1.0 --scheduled=true -n kharon-operator-tests
oc tag quay.io/cvicensa/kharon-test:v1.2.0 kharon-operator-tests/kharon-test:v1.2.0 --scheduled=true -n kharon-operator-tests

sleep 10

oc new-app kharon-operator-tests/kharon-test:v1.0.0 --name kharon-test-v1-0-0 -n kharon-operator-tests
oc new-app kharon-operator-tests/kharon-test:v1.1.0 --name kharon-test-v1-1-0 -n kharon-operator-tests
oc new-app kharon-operator-tests/kharon-test:v1.2.0 --name kharon-test-v1-2-0 -n kharon-operator-tests