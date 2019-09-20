# Preparation

## Load environment variables

```sh
. ./env.sh
```

## Login to your Openshift Cluster

```sh
oc login ...
```

## Create a project for monitoring

```
oc new-project monitoring
```

## Install both Prometheus and Grafana operators in project monitoring

This needs to be done from the Openshift console

> **WARNING 1!** Current version of Prometheus (version 0.27.0) has a buggy StatefulSet configuration... You have to change the memory assigned to container `rules-configmap-reloader` from 10Mi to 25Mi or more

> **WARNING 2!** It's necessary to adjust permission for the Prometheus Operator service account, for instance"

```sh
oc adm policy add-cluster-role-to-user view system:serviceaccount:monitoring:prometheus-k8s
```

## Deploy monitoring artifacts

```
oc apply -f ./deploy/prometheus -n monitoring
oc apply -f ./deploy/grafana -n monitoring
```

# Building and pushing the image of the operator if needed

```
./build-operator.sh && ./push-operator.sh
```

# Deploy the demo

The goal of this step is twofold. On one hand *deploying three releases of a test applicacion `kharon-test`* (a simple Spring Boot application with a even simple API `/api/greeting`), and on the other hand *deploying **Kharon Operator***.

In order to this we have prepared a script `deploy-demo.sh`, this script does the following:

* Creates Image Streams pointing to images of the test application in [quay.io](https://quay.io)
* Deploys the test application three times (one for each release). In a real situation you would eventually release and deploy in different points in time but we're doing it at once for the sake of simplicity
* Expose the services so that you can try each release separately before actually exposing it to the real traffic. This is indeed optional but part of the script.
* Label the Service object as `team=spring-boot-actuator` so that Prometheus can scrape metrics from them
* Finally it deploys the operator itself

Now please run the next command.

```sh
./deploy-demo.sh
```

Release names are as follows:

* v1.0.0 as kharon-test-v1-0-0
* v1.1.0 as kharon-test-v1-1-0
* v1.2.0 as kharon-test-v1-2-0

After running the deploy command you should be able to see this pods running.

```sh
oc get pod -n $PROJECT_NAME
```

# Running the demo

So far we have prepared the environment to run our demo, next steps will actually run the `Kharon Operator` demo.

## Phase 1 - Create a Canary Object

We have prepared a Canary object for this demo named `kharon-test`. In general the idea is create a Canary object and modify it everytime there is a new release we want to deploy as a canary. For the sake of simplicity we have created three different descriptors for the same Canary object, the only difference between them is the name of the target resource (in this case a `DeploymentConfig`).

But before we create the Canary object, let's check the Route available in our project `kharon-tests`.

```sh
 oc get route -n $PROJECT_NAME 
NAME                 HOST/PORT                                                                                       PATH   SERVICES             PORT       TERMINATION   WILDCARD
kharon-test-v1-0-0   kharon-test-v1-0-0-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-0-0   8080-tcp                 None
kharon-test-v1-1-0   kharon-test-v1-1-0-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-1-0   8080-tcp                 None
kharon-test-v1-2-0   kharon-test-v1-2-0-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-2-0   8080-tcp                 None
```

For now, let's create the Canary object.

```sh
oc apply -f ./example/kharon-test-v1-0-0.yaml
```

Because this is the first time our operator will create a Route called as the ServiceName property in the Canary object. In this case the name of the route should be `kharon-test`. Also because it's the first time... there's no actual Canary release so all the traffic will be routed to the Service of the first release, that is `kharon-test-v1-0-0`. Now is you get the Routes again you should get a new one as described before.

```sh
oc get route -n $PROJECT_NAME 
NAME                 HOST/PORT                                                                                       PATH   SERVICES             PORT       TERMINATION   WILDCARD
kharon-test          kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com                 kharon-test-v1-0-0   8080-tcp                 None
kharon-test-v1-0-0   kharon-test-v1-0-0-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-0-0   8080-tcp                 None
kharon-test-v1-1-0   kharon-test-v1-1-0-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-1-0   8080-tcp                 None
kharon-test-v1-2-0   kharon-test-v1-2-0-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-2-0   8080-tcp                 None
```

Test the route as follows:

> As you can check the object returned states that `version` is the one referred to by the Canary object (`spec->targetRef->name: kharon-test-v1-0-0`).

```sh
curl http://kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com/api/greeting
{"content":"Hello, World!","version":"v1.0.0"}
```

Now let's imagine we fast forward a number of weeks/days and we have our new release `v1.1.0` deployed and ready to become a Canary release.

## Phase 2 - Deploying our first actual Canary release

In this case the idea is testing the rollback feature, for that reason we need to introduce errors after kicking off the new canary release.

For this scenario we have prepared a second descriptor `./example/kharon-test-v1-1-0.yaml` for the new release `v1.1.0`.

> **Remember that although a different file... it's acually the same Canary object!**

But before applying this descriptor let's prepare the test.

### Generate load

Run the following command to generate some load.

> Load is generated from a pod run as Kubernetes Job

```sh
oc delete job kharon-gen-req ;  oc apply -f example/kharon-gen-req-job.yaml -n $PROJECT_NAME 
```

### Trigger the canary relase for v1.1.0

```sh
oc apply -f ./example/kharon-test-v1-1-0.yaml
```

Wait for some seconds and check the status of the canary

> Look for properties like *Canary Metric Value*, *Canary Weight*, *Failed Checks* or *Is Canary Running*.

```sh
oc describe canary kharon-test -n $PROJECT_NAME
```

### Generate errors

Run the following command to generate some errors.

```sh
oc delete job kharon-gen-err-v1-1-0 ;  oc apply -f example/kharon-gen-err-v1-1-0-job.yaml -n $PROJECT_NAME 
```

### Check the status of the route 

In a different terminal run this command to watch the status of the Route.

> In this case we see the canary start at 10%, then progress to 20% and finally go back to the previous release

```sh
oc get route kharon-test -w

NAME          HOST/PORT                                                                                PATH   SERVICES                                          PORT       TERMINATION   WILDCARD
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-0-0(90%),kharon-test-v1-1-0(10%)   8080-tcp                 None
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com         kharon-test-v1-0-0(80%),kharon-test-v1-1-0(20%)   8080-tcp         None
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com         kharon-test-v1-0-0   8080-tcp         None

```

In another terminal or after Ctrl+C the current one describe the canary to see its status.

> Pay special attention to the `Events` area at the end.

```sh
oc describe canary kharon-test
Name:         kharon-test
...
Kind:         Canary
...
Spec:
...
Status:
  Canary Metric Value:  0
  Canary Weight:        0
  Failed Checks:        0
  Is Canary Running:    false
  Iterations:           0
  Last Action:          RequeueEvent
  Last Applied Spec:    0
  Last Promoted Spec:   0
  Last Step Time:       2019-09-18T21:30:51Z
  Last Update:          2019-09-18T21:31:21Z
  Reason:               Realease was rolled back
  Release History:
    Ref:
      API Version:  apps.openshift.io/v1
      Kind:         DeploymentConfig
      Name:         kharon-test-v1-0-0
    Id:             kharon-test-v1-0-0
    Name:           kharon-test-v1-0-0
  Status:           Failure
Events:
  Type     Reason                 Age   From               Message
  ----     ------                 ----  ----               -------
  Normal   CreatePrimaryRelease   111s  canary_controller  Primary release deployed from kharon-test-v1-0-0
  Normal   ProgressCanaryRelease  93s   canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-1-0 to 10%
  Normal   ProgressCanaryRelease  33s   canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-1-0 to 20%
  Warning  RollbackReleaseStart   3s    canary_controller  Canary release rollback triggered for kharon-test
  Warning  ProcessingError        3s    canary_controller  Realease was rolled back
```
## Phase 3 - Deploying our third Canary release

Again run the next command to update the Target Ref from v1.0.0 to v1.2.0.

```sh
oc apply -f ./example/kharon-test-v1-2-0.yaml
```

And again monitor the status of the route...

```sh
oc get route kharon-test -w
NAME          HOST/PORT                                                                                PATH   SERVICES                                          PORT       TERMINATION   WILDCARD
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com          kharon-test-v1-0-0(90%),kharon-test-v1-2-0(10%)   8080-tcp                 None
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com         kharon-test-v1-0-0(80%),kharon-test-v1-2-0(20%)   8080-tcp         None
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com         kharon-test-v1-0-0(70%),kharon-test-v1-2-0(30%)   8080-tcp         None
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com         kharon-test-v1-0-0(60%),kharon-test-v1-2-0(40%)   8080-tcp         None
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com         kharon-test-v1-0-0(0%),kharon-test-v1-2-0(100%)   8080-tcp         None
kharon-test   kharon-test-kharon-operator-tests.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com         kharon-test-v1-2-0   8080-tcp         None
```

As we did before have a look to the `Events` area and `Status->Release History`

```sh
Name:         kharon-test
...
Kind:         Canary
...
Spec:
...
Status:
  Canary Metric Value:  0
  Canary Weight:        0
  Failed Checks:        0
  Is Canary Running:    false
  Iterations:           6
  Last Action:          EndCanaryRelease
  Last Applied Spec:    0
  Last Promoted Spec:   0
  Last Step Time:       <nil>
  Last Update:          2019-09-18T21:43:26Z
  Release History:
    Ref:
      API Version:  apps.openshift.io/v1
      Kind:         DeploymentConfig
      Name:         kharon-test-v1-0-0
    Id:             kharon-test-v1-0-0
    Name:           kharon-test-v1-0-0
    Ref:
      API Version:  apps.openshift.io/v1
      Kind:         DeploymentConfig
      Name:         kharon-test-v1-2-0
    Id:             kharon-test-v1-2-0
    Name:           kharon-test-v1-2-0
  Status:           True
Events:
  Type     Reason                 Age    From               Message
  ----     ------                 ----   ----               -------
  Normal   CreatePrimaryRelease   14m    canary_controller  Primary release deployed from kharon-test-v1-0-0
  Normal   ProgressCanaryRelease  14m    canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-1-0 to 10%
  Normal   ProgressCanaryRelease  13m    canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-1-0 to 20%
  Warning  RollbackReleaseStart   12m    canary_controller  Canary release rollback triggered for kharon-test
  Warning  ProcessingError        12m    canary_controller  Realease was rolled back
  Normal   RollbackReleaseEnd     12m    canary_controller  Instance kharon-test was rollback from kharon-test-v1-1-0 to kharon-test-v1-0-0
  Normal   ProgressCanaryRelease  5m32s  canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-2-0 to 10%
  Normal   ProgressCanaryRelease  4m32s  canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-2-0 to 20%
  Normal   ProgressCanaryRelease  3m31s  canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-2-0 to 30%
  Normal   ProgressCanaryRelease  2m31s  canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-2-0 to 40%
  Normal   ProgressCanaryRelease  91s    canary_controller  Canary release kharon-test progressed deployment kharon-test-v1-2-0 to 100%
  Normal   EndCanaryRelease       31s    canary_controller  Canary release kharon-test ended deployment kharon-test-v1-2-0 with success
```

As you can check this it all worked out perfectly well!