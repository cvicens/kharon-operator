# Preparation

## Create a project for monitoring

```
oc new-project monitoring
```

## Install both Prometheus and Grafana operators in project monitoring

This needs to be done from the Openshift console

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

```
./deploy-demo.sh
```

# Adapt the demo CR

Pay attention to the metricsServer

```yaml
apiVersion: kharon.redhat.com/v1alpha1
kind: Canary
metadata:
  name: canary-kharon-test
  labels:
    app: kharon-test
spec:
  serviceName: kharon-test
  enabled: true
  type: Native
  canaryAnalysis:
    metricsServer: 'http://prometheus-operated-monitoring.apps.cluster-kharon-eeae.kharon-eeae.open.redhat.com'
    # schedule interval (default 60s)
    interval: 60
    # max number of failed metric checks before rollback
    threshold: 3
    # max traffic percentage routed to canary percentage (0-100)
    maxWeight: 50
    # canary increment step percentage (0-100)
    stepWeight: 10
    metric:
      name: error-rate
      # max error rate (5xx responses)
      # percentage (0-100)
      threshold: 2
      operator: 'lt'
      interval: 10
      prometheusQuery: '(sum(api_http_errors_total{namespace="{{.Namespace}}",service="{{.Spec.TargetRef.Name}}"})/sum(api_http_requests_total{namespace="{{.Namespace}}",service="{{.Spec.TargetRef.Name}}"}))*100.0'
      
  targetRefContainerPort: '8080-tcp' # If you don't specify this... maybe the order of ports is not correct and you'll get another port...
  targetRef:
    apiVersion: apps.openshift.io/v1
    kind: DeploymentConfig
    name: kharon-test-v1-0-0
```

