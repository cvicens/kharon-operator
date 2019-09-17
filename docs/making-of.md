# Works with master branch 

```
$ operator-sdk version
operator-sdk version: v0.8.0-1-gf7f6440, commit: f7f64400809897a9b21f2c813a4c6e775cc069bc
```

# prep for go modules

```
cd $GOPATH
export GO111MODULE=on
```

# Create new operator

```
export OPERATOR_NAME="kharon-operator"
export API_VERSION="cloudnative.redhat.com/v1alpha1"

mkdir -p $GOPATH/src/github.com/redhat

cd $GOPATH/src/github.com/redhat

operator-sdk new ${OPERATOR_NAME} --type=go --skip-git-init

cd ./${OPERATOR_NAME}

operator-sdk add api --api-version=${API_VERSION} --kind=ServiceConfig

operator-sdk add controller --api-version=${API_VERSION} --kind=ServiceConfig
```

# Modify types

code `./pkg/apis/app/<version>/<kind>_types.go`

# Generate types

```
operator-sdk generate k8s
```

# Run this everytime you import a new module

```
go mod vendor
```

## List module versions

```
go list -m -versions gopkg.in/src-d/go-git.v4
```

# Build and run the operator
## Run locally

```
export PROJECT_NAME=${OPERATOR_NAME}-tests
oc new-project ${PROJECT_NAME}

oc apply -f deploy/service_account.yaml 
oc apply -f deploy/role.yaml
oc apply -f deploy/role_binding.yaml

oc apply -f deploy/crds/cloudnative_v1alpha1_serviceconfig_crd.yaml
oc apply -f deploy/crds/cloudnative_v1alpha1_serviceconfig_cr.yaml

operator-sdk up local --namespace=${PROJECT_NAME}
```

## Build

```
export QUAY_USERNAME=cvicensa
export OPERATOR_VERSION=0.0.1
operator-sdk build quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:v${OPERATOR_VERSION}
docker push quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:v${OPERATOR_VERSION}
```

## Change operator.yaml

```
//cat deploy/operator.yaml | sed "s|REPLACE_IMAGE|quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:v${OPERATOR_VERSION}|g" > deploy/operator-v${OPERATOR_VERSION}.yaml
sed -i "" "s|REPLACE_IMAGE|quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:v${OPERATOR_VERSION}|g" deploy/operator.yaml
```

## Push image

```
docker push quay.io/${QUAY_USERNAME}/${OPERATOR_NAME}:v${OPERATOR_VERSION}
```

## Deploy the operator manually

```
oc apply -f deploy/operator.yaml
```

# Manage the operator using the Operator Lifecycle Manager

## Generate an operator Cluster Service Version (CSV) manifest
operator-sdk olm-catalog gen-csv --csv-version ${OPERATOR_VERSION}

## Deploy the operator

First undeploy the manually deployed operator

oc delete -f deploy/operator.yaml

### Create an OperatorGroup

/// Careful...
cat <<EOF | oc create -n ${PROJECT_NAME} -f -
apiVersion: operators.coreos.com/v1alpha2
kind: OperatorGroup
metadata:
  name: ${OPERATOR_NAME}-group
  namespace: ${PROJECT_NAME}
  spec:
    targetNamespaces:
    - ${PROJECT_NAME}
EOF

### Create a CSV
sed -e "s|REPLACE_NAMESPACE|${PROJECT_NAME}|g" deploy/olm-catalog/${OPERATOR_NAME}/${OPERATOR_VERSION}/${OPERATOR_NAME}.v${OPERATOR_VERSION}.clusterserviceversion.yaml > deploy/olm-catalog/${OPERATOR_NAME}/${OPERATOR_VERSION}/${OPERATOR_NAME}.v${OPERATOR_VERSION}.clusterserviceversion-${PROJECT_NAME}.yaml
oc apply -f deploy/olm-catalog/${OPERATOR_NAME}/${OPERATOR_VERSION}/${OPERATOR_NAME}.v${OPERATOR_VERSION}.clusterserviceversion-${PROJECT_NAME}.yaml
oc get ClusterServiceVersion ${OPERATOR_NAME}.v${OPERATOR_VERSION} -o json | jq '.status'

### Create a subscription
sed -e "s|REPLACE_NAMESPACE|${PROJECT_NAME}|g" deploy/${OPERATOR_NAME}-subscription.yaml > deploy/${OPERATOR_NAME}-subscription-${PROJECT_NAME}.yaml
oc apply -f deploy/${OPERATOR_NAME}-subscription-${PROJECT_NAME}.yaml



# Modules See:
https://github.com/golang/go/wiki/Modules#example

# Links
https://blog.openshift.com/kubernetes-operators-best-practices/
https://banzaicloud.com/blog/operator-sdk/
https://github.com/operator-framework/operator-sdk/blob/master/doc/user-guide.md
https://www.tailored.cloud/kubernetes/write-a-kubernetes-controller-operator-sdk/
https://flugel.it/building-custom-kubernetes-operators-part-3-building-operators-in-go-using-operator-sdk/
https://itnext.io/debug-a-go-application-in-kubernetes-from-ide-c45ad26d8785


# Canary Release lifecycle

## Openshift Native Canary Release

This Canary release is based on Openshift Routes.

At the beginning a new Deployment (or DeploymentConfig) is created.

Then you create a Canary (CR) object that points to this Deployment, this means that currentRelease attribute contains the ID of the Release in the Releases array, and there is a referrence object in the array pointing to the real object.

(What if we had a targetRef instead of current release... then we feed the array of releases in status, )

In this case, because there are no previous releases... this Canary should be exposed completely through the corresponding route, becoming primary at once.

We check that this is so...

```
{"spec":{"to": {"kind": "Service","name": "ab-example-a","weight": 100}, "alternateBackends": []}}

or 

{"spec":{"to": {"kind": "Service","name": "ab-example-a","weight": 100}}}
```

# Prepare the test

Create a new DC:

```
oc new-app openshift/deployment-example:v1 --name=deployment-example-v1
```

# Build to ext reg

```
oc new-project ext-registry
```

```
oc tag registry.access.redhat.com/openjdk/openjdk-11-rhel7:latest openshift/openjdk-11-rhel7:latest --scheduled=true -n ext-registry
```

```
oc create secret docker-registry quay-dockercfg --docker-server=quay.io --docker-username=cvicensa --docker-password=<password> --docker-email=cvicensa@redhat.com

or 

oc create secret docker-registry quay-dockercfg --docker-server=https://quay.io --docker-username=cvicensa --docker-password=<password> --docker-email=cvicensa@redhat.com
```

```
oc edit sa builder

===

apiVersion: v1
imagePullSecrets:
- name: builder-dockercfg-bd6lp
kind: ServiceAccount
metadata:
  creationTimestamp: "2019-08-21T22:13:15Z"
  name: builder
  namespace: ext-registry
  resourceVersion: "4408940"
  selfLink: /api/v1/namespaces/ext-registry/serviceaccounts/builder
  uid: df7a527e-c460-11e9-b595-063f19d9dd24
secrets:
- name: builder-token-9svbf
- name: builder-dockercfg-bd6lp
- name: quay-dockercfg
```

```
oc new-build --name openshift-quickstarts-ext-bc openshift/openjdk-11-rhel7:latest~https://github.com/jboss-openshift/openshift-quickstarts --context-dir=undertow-servlet -n ext-registry
```

```
oc edit bc/openshift-quickstarts-ext-bc

===

apiVersion: build.openshift.io/v1
kind: BuildConfig
metadata:
  annotations:
    openshift.io/generated-by: OpenShiftNewBuild
  creationTimestamp: "2019-08-21T22:32:32Z"
  labels:
    build: openshift-quickstarts-ext-bc
  name: openshift-quickstarts-ext-bc
  namespace: ext-registry
  resourceVersion: "4413649"
  selfLink: /apis/build.openshift.io/v1/namespaces/ext-registry/buildconfigs/openshift-quickstarts-ext-bc
  uid: 90c59a59-c463-11e9-baac-0a580a820021
spec:
  failedBuildsHistoryLimit: 5
  nodeSelector: null
  output:
    pushSecret:
      name: quay-dockercfg
    to:
      kind: DockerImage
      name: quay.io/cvicensa/openshift-quickstarts:latest
  postCommit: {}
  resources: {}
  runPolicy: Serial
  source:
    contextDir: undertow-servlet
    git:
      uri: https://github.com/jboss-openshift/openshift-quickstarts
    type: Git
  strategy:
    sourceStrategy:
      from:
        kind: ImageStreamTag
        name: openjdk-11-rhel7:latest
        namespace: openshift
    type: Source
  successfulBuildsHistoryLimit: 5
  triggers:
  - github:
      secret: sXew99-RfH55NanDcvO7
    type: GitHub
  - generic:
      secret: TrHF_BuBfyZCDw7KRMwz
    type: Generic
  - type: ConfigChange
  - imageChange:
      lastTriggeredImageID: registry.access.redhat.com/openjdk/openjdk-11-rhel7@sha256:cabd10fa28a59f646111f0d9dc9596ba6b6e5bb2fb41f0994272e37b1f1036e3
    type: ImageChange

```

```
oc start-build openshift-quickstarts-ext-bc -n ext-registry
```

```
oc logs -f bc/openshift-quickstarts-ext-bc
```

```
oc tag quay.io/cvicensa/openshift-quickstarts:latest ext-registry/openshift-quickstarts:latest --scheduled=true -n ext-registry
```

```
oc secrets link default quay-dockercfg --for=pull
```


```
oc new-app ext-registry/openshift-quickstarts:latest
```

```
oc expose svc/openshift-quickstarts
```