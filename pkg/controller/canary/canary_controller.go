package canary

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"

	kharonv1alpha1 "github.com/redhat/kharon-operator/pkg/apis/kharon/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1 "k8s.io/api/apps/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
	record "k8s.io/client-go/tools/record"

	oappsv1 "github.com/openshift/api/apps/v1"
	routev1 "github.com/openshift/api/route/v1"

	// Custom metrics
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	// Util
	_util "github.com/redhat/kharon-operator/pkg/util"
	_metrics "github.com/redhat/kharon-operator/pkg/util/metrics"
)

// Operator Name
const operatorName = "KharonOperator"

// Best practices
const controllerName = "canary_controller"

const (
	errorTargetRefEmpty                   = "Not a proper Canary object because TargetRef is empty"
	errorTargetRefContainerPortEmpty      = "Not a proper Canary object because TargetRefContainerPort is empty"
	errorTargetRefKind                    = "Not a proper Canary object because TargetRef is not Deployment or DeploymentConfig"
	errorServiceNameEmpty                 = "Not a proper Canary object because ServiceName is empty"
	errorCanaryAnalysisEmpty              = "Not a proper Canary object because CanaryAnalysis is empty"
	errorTargetRefNotValid                = "Not a proper Canary object because TargetRef points to an invalid object"
	errorNotACanaryObject                 = "Not a Canary object"
	errorCanaryObjectNotValid             = "Not a valid Canary object"
	errorRouteNotFound                    = "Route object was deleted or cannot be found"
	errorUnexpected                       = "Unexpected error"
	errorCanaryWeightNot100               = "Canary has not reached 100%"
	errorQueryingMetricsServer            = "Error when querying the metrics server"
	errorExtractingValueFromMetricsResult = "Error extracting metric value"
	errorMountingMetricsURL               = "Error when mounting the metrics URL"
	errorNoReleaseInHistoryToRollback     = "No release in history to rollback"
	errorUnableToUpdateInstance           = "Unable to update instance"
	errorUnableToUpdateStatus             = "Unable to update status"
	errorRolledbackRelease                = "Realease was rolled back"
	warningCanaryAlreadyEnded             = "Canary already reached 100%"
)

var log = logf.Log.WithName("controller_canary")

// Custom metrics
var (
	currentCanaryWeight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kharon_current_canary_weight",
		Help: "Weight of the current canary release",
	},
		[]string{
			"namespace",
			"canary",
			"target",
		})
	currentCanaryMetricValue = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kharon_current_canary_metric_value",
		Help: "Metric Value of the current canary release",
	},
		[]string{
			"namespace",
			"canary",
			"target",
		})
)

// TargetServiceDef collects data to create a Service
type TargetServiceDef struct {
	serviceName string
	namespace   string
	selector    map[string]string
	portName    string
	protocol    corev1.Protocol
	port        int32
	targetPort  intstr.IntOrString
}

// DestinationServiceDef collects data to define To and/or AlternateBackEnds in Route
type DestinationServiceDef struct {
	Name   string
	Weight int32
}

// TargetRouteDef collects data to create a Route
type TargetRouteDef struct {
	routeName      string
	namespace      string
	selector       map[string]string
	targetPort     intstr.IntOrString
	primaryService *DestinationServiceDef
	canaryService  *DestinationServiceDef
}

// Add creates a new Canary Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	scheme := mgr.GetScheme()
	oappsv1.AddToScheme(scheme)
	routev1.AddToScheme(scheme)
	// Best practices
	return &ReconcileCanary{client: mgr.GetClient(), scheme: scheme, recorder: mgr.GetRecorder(controllerName)}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("canary-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	predicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Check that new and old objects are the expected type
			_, ok := e.ObjectOld.(*kharonv1alpha1.Canary)
			if !ok {
				log.Error(nil, "Update event has no old proper runtime object to update", "event", e)
				return false
			}
			newServiceConfig, ok := e.ObjectNew.(*kharonv1alpha1.Canary)
			if !ok {
				log.Error(nil, "Update event has no proper new runtime object for update", "event", e)
				return false
			}
			if !newServiceConfig.Spec.Enabled {
				log.Error(nil, "Runtime object is not enabled", "event", e)
				return false
			}

			// Also check ff no change in ResourceGeneration to return false
			if e.MetaOld == nil {
				log.Error(nil, "Update event has no old metadata", "event", e)
				return false
			}
			if e.MetaNew == nil {
				log.Error(nil, "Update event has no new metadata", "event", e)
				return false
			}
			if e.MetaNew.GetGeneration() == e.MetaOld.GetGeneration() {
				return false
			}

			return true
		},
		CreateFunc: func(e event.CreateEvent) bool {
			log.Info("canaryOk (predicate->CreateFunc)")
			_, ok := e.Object.(*kharonv1alpha1.Canary)
			if !ok {
				return false
			}

			return true
		},
	}

	// Watch for changes to primary resource Canary
	err = c.Watch(&source.Kind{Type: &kharonv1alpha1.Canary{}}, &handler.EnqueueRequestForObject{}, predicate)
	if err != nil {
		return err
	}

	// TODO(user): Modify this to be the types you create that are owned by the primary resource
	// Watch for changes to secondary resource Pods and requeue the owner Canary
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &kharonv1alpha1.Canary{},
	})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileCanary implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileCanary{}

// ReconcileCanary reconciles a Canary object
type ReconcileCanary struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
	// Best practices...
	recorder record.EventRecorder
}

// Reconcile reads that state of the cluster for a Canary object and makes changes based on the state read
// and what is in the Canary.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileCanary) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Canary")

	// Fetch the Canary instance
	instance := &kharonv1alpha1.Canary{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Validate the CR instance
	if ok, err := r.IsValid(instance); !ok {
		return r.ManageError(instance, err)
	}

	// Search for the target ref
	var target runtime.Object
	var targetRef = instance.Spec.TargetRef
	switch kind := instance.Spec.TargetRef.Kind; kind {
	case "Deployment":
		target = &appsv1.Deployment{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: targetRef.Name, Namespace: request.NamespacedName.Namespace}, target)
		if err != nil && errors.IsNotFound(err) {
			log.Info(fmt.Sprintf("Target Deployment was not found!"))
			return r.ManageError(instance, err)
		} else if err != nil {
			return r.ManageError(instance, err)
		}
	case "DeploymentConfig":
		target = &oappsv1.DeploymentConfig{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: targetRef.Name, Namespace: request.NamespacedName.Namespace}, target)
		if err != nil && errors.IsNotFound(err) {
			log.Info(fmt.Sprintf("Target DeploymentConfig was not found!"))
			return r.ManageError(instance, err)
		} else if err != nil {
			return r.ManageError(instance, err)
		}
	default:
		log.Info("==== isOther ====" + kind)
	}

	//log.Info(fmt.Sprintf("==== target ==== %s", target))

	// Now that we have a target let's initialize the CR instance
	if initialized, err := r.IsInitialized(instance, target); err == nil && !initialized {
		err := r.client.Update(context.TODO(), instance)
		if err != nil {
			log.Error(err, errorUnableToUpdateInstance, "instance", instance)
			return r.ManageError(instance, err)
		}
		return reconcile.Result{}, nil
	} else {
		if err != nil {
			return r.ManageError(instance, err)
		}
	}

	// If reentering from a canary rollback
	if instance.Status.Status == kharonv1alpha1.CanaryConditionStatusFailure && instance.Status.Reason == errorRolledbackRelease {
		// If target is already pointing to the previous release, we're fine
		if instance.Spec.TargetRef == instance.Status.ReleaseHistory[len(instance.Status.ReleaseHistory)-1].Ref {
			return r.ManageSuccess(instance, 0, kharonv1alpha1.NoAction)
		}

		// Else... we need to update TargetRef to point to the current release (hence rollback)
		fromTarget := instance.Spec.TargetRef
		instance.Spec.TargetRef = instance.Status.ReleaseHistory[len(instance.Status.ReleaseHistory)-1].Ref
		if err := r.client.Update(context.TODO(), instance); err != nil {
			log.Error(err, errorUnableToUpdateInstance, "instance", instance)
			return r.ManageError(instance, err)
		}
		// Send notification event
		r.recorder.Eventf(instance, "Normal", "CanaryRollback", "Instance %s was rollback from %s to %s", instance.ObjectMeta.Name, fromTarget.Name, instance.Spec.TargetRef.Name)
		return reconcile.Result{}, nil
	}

	// Canary is inititialized, target is fine... cotainer, port... all OK

	// First we have to figure out what action to trigger

	// If there's no Primary
	if len(instance.Status.ReleaseHistory) <= 0 {
		// Then Primary is the TargetRef ==> Action: Create Primary Release (and leave)
		return r.CreatePrimaryRelease(instance)
	} else {
		// Else, there's Primary

		// If TargetRef is different
		if instance.Spec.TargetRef != instance.Status.ReleaseHistory[len(instance.Status.ReleaseHistory)-1].Ref {

			// Then TargetRef is a Canary (a Canary IS already running OR starting)

			// If Canary metric is not met, increase failedCheck counter
			if metricValue, err := _metrics.ExecuteMetricQuery(instance); err == nil {
				currentCanaryMetricValue.WithLabelValues(instance.Namespace, instance.Name, instance.Spec.TargetRef.Name).Set(metricValue)
				instance.Status.CanaryMetricValue = metricValue
				if !_metrics.ValidateMetricValue(metricValue, instance.Spec.CanaryAnalysis.Metric.Operator, instance.Spec.CanaryAnalysis.Metric.Threshold) {
					instance.Status.FailedChecks++
				}

			} else {
				log.Error(err, fmt.Sprintf("Error %s", err))
			}

			// If failedCheck threshold is met, rollback
			if instance.Status.FailedChecks > instance.Spec.CanaryAnalysis.Threshold {
				return r.RollbackRelease(instance)
			}

			// If it's been more than the interval beween Canary steps
			timeSinceLastStep := time.Since(instance.Status.LastStepTime.Time)
			if timeSinceLastStep > time.Duration(instance.Spec.CanaryAnalysis.Interval)*time.Second {
				// If Progress is < 100 % ==> Action: Progress Canary Release
				if instance.Status.CanaryWeight < 100 {
					return r.ProgressCanaryRelease(instance)
				}
				// Else ==> Action: End Canary Release ==> Action Create Primary Release From Canary
				return r.EndCanaryRelease(instance)
			} else {
				return r.ManageSuccess(instance, time.Duration(instance.Spec.CanaryAnalysis.Metric.Interval)*time.Second, kharonv1alpha1.RequeueEvent)
			}
		} else {
			// If TargetRef is the same ==> Action: No Action ==> it means reset status to zero (so to speak) if it's not zero
			log.Info("ACTION {NO_ACTION}")
			return r.ManageSuccess(instance, 0, kharonv1alpha1.NoAction)
		}
	}
}

// CreatePrimaryRelease creates new release, hence no canary is triggered
func (r *ReconcileCanary) CreatePrimaryRelease(instance *kharonv1alpha1.Canary) (reconcile.Result, error) {
	log.Info("ACTION {CREATE_PRIMARY_RELEASE}")
	// Create a Service for TargetRef
	targetService, err := r.CreateServiceForTargetRef(instance)
	if err != nil && !errors.IsAlreadyExists(err) {
		return r.ManageError(instance, err)
	}

	// Create a Route that points to the targetService with no alternate service
	primaryService := &DestinationServiceDef{
		Name:   targetService.Name,
		Weight: 100,
	}
	canaryService := &DestinationServiceDef{}
	if route, err := r.CreateRouteForCanary(instance, primaryService, canaryService); err != nil {
		if errors.IsAlreadyExists(err) {
			if _, err := r.UpdateRouteDestinationsForCanary(route, primaryService, canaryService); err != nil {
				return r.ManageError(instance, err)
			}
		}
	}

	// Update Status with new Release!
	instance.Status.IsCanaryRunning = false
	instance.Status.CanaryWeight = 0
	instance.Status.Iterations = 0
	instance.Status.ReleaseHistory = append(instance.Status.ReleaseHistory, kharonv1alpha1.Release{
		ID:   instance.Spec.TargetRef.Name,
		Name: instance.Spec.TargetRef.Name,
		Ref:  instance.Spec.TargetRef,
	})

	// Send notification event
	r.recorder.Eventf(instance, "Normal", string(kharonv1alpha1.CreatePrimaryRelease), "Primary release deployed from %s", instance.Spec.TargetRef.Name)

	return r.ManageSuccess(instance, time.Duration(instance.Spec.CanaryAnalysis.Interval)*time.Second, kharonv1alpha1.CreatePrimaryRelease)
}

// RollbackRelease goes back to the previous release in the release history
func (r *ReconcileCanary) RollbackRelease(instance *kharonv1alpha1.Canary) (reconcile.Result, error) {
	log.Info("ACTION {ROLLBACK_RELEASE}")
	if len(instance.Status.ReleaseHistory) <= 0 {
		return r.ManageError(instance, _util.NewError(errorNoReleaseInHistoryToRollback))
	}

	// Fetch route
	route, err := r.FetchRoute(instance)
	if err != nil {
		log.Error(err, errorRouteNotFound)
		return r.ManageError(instance, err)
	}

	// Route should point to current release (latest in history) with 100% Weight
	primaryService := &DestinationServiceDef{
		Name:   instance.Status.ReleaseHistory[len(instance.Status.ReleaseHistory)-1].Name,
		Weight: 100,
	}
	canaryService := &DestinationServiceDef{}
	if _, err := r.UpdateRouteDestinationsForCanary(route, primaryService, canaryService); err != nil {
		return r.ManageError(instance, err)
	}

	// Update Status with new Release!
	instance.Status.IsCanaryRunning = false
	instance.Status.CanaryWeight = 0
	instance.Status.Iterations = 0
	instance.Status.FailedChecks = 0
	instance.Status.CanaryMetricValue = 0

	// Send notification event
	r.recorder.Eventf(instance, "Warning", string(kharonv1alpha1.RollbackRelease), "Canary release rollback triggered for %s", instance.ObjectMeta.Name)

	return r.ManageError(instance, _util.NewError(errorRolledbackRelease))
}

// FetchRoute get the route related to the canary object
func (r *ReconcileCanary) FetchRoute(instance *kharonv1alpha1.Canary) (*routev1.Route, error) {
	route := &routev1.Route{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.ServiceName, Namespace: instance.Namespace}, route)
	if err != nil {
		return nil, err
	}

	return route, nil
}

// ProgressCanaryRelease progresses the canary by updating its weight
func (r *ReconcileCanary) ProgressCanaryRelease(instance *kharonv1alpha1.Canary) (reconcile.Result, error) {
	log.Info("ACTION {PROGRESS_CANARY_RELEASE}")
	// If Canary Weight is already >= 100, then we produce a warning
	if instance.Status.CanaryWeight >= 100 {
		err := errors.NewBadRequest(warningCanaryAlreadyEnded)
		log.Error(err, warningCanaryAlreadyEnded)
		return r.ManageError(instance, err)
	}

	// Let's calculate the next weight
	canaryWeight := instance.Status.CanaryWeight + instance.Spec.CanaryAnalysis.StepWeight
	// If new Canary weight is >= MaxWeigh, then set it to 100
	if canaryWeight >= instance.Spec.CanaryAnalysis.MaxWeight {
		canaryWeight = 100
	}

	// Fetch route
	route, err := r.FetchRoute(instance)
	if err != nil {
		log.Error(err, errorRouteNotFound)
		return r.ManageError(instance, err)
	}

	// Route should point to current release (latest in history) (100 - Canary Weight) and the TargetRef (Canary Weight)
	primaryService := &DestinationServiceDef{
		Name:   instance.Status.ReleaseHistory[len(instance.Status.ReleaseHistory)-1].Name,
		Weight: (100 - canaryWeight),
	}
	canaryService := &DestinationServiceDef{
		Name:   instance.Spec.TargetRef.Name,
		Weight: instance.Status.CanaryWeight,
	}
	if _, err := r.UpdateRouteDestinationsForCanary(route, primaryService, canaryService); err != nil {
		return r.ManageError(instance, err)
	}

	// Update Status with our progressed Canary
	instance.Status.IsCanaryRunning = true
	instance.Status.CanaryWeight = canaryWeight
	instance.Status.Iterations++
	instance.Status.LastStepTime = metav1.Now()

	currentCanaryWeight.WithLabelValues(instance.Namespace, instance.Name, instance.Spec.TargetRef.Name).Set(float64(canaryWeight))

	// Send notification event
	r.recorder.Eventf(instance, "Normal", string(kharonv1alpha1.ProgressCanaryRelease), "Canary release %s progressed deployment %s to %d%%", instance.ObjectMeta.Name, instance.Spec.TargetRef.Name, canaryWeight)

	return r.ManageSuccess(instance, time.Duration(instance.Spec.CanaryAnalysis.Metric.Interval)*time.Second, kharonv1alpha1.ProgressCanaryRelease)
}

// EndCanaryRelease ends the canary because everything went fine... so canary becomes primary
func (r *ReconcileCanary) EndCanaryRelease(instance *kharonv1alpha1.Canary) (reconcile.Result, error) {
	log.Info("ACTION {END_CANARY_RELEASE}")
	// If Canary Weight is already < 100, then we produce a warning
	if instance.Status.CanaryWeight < 100 {
		err := errors.NewBadRequest(errorCanaryWeightNot100)
		log.Error(err, errorCanaryWeightNot100)
		return r.ManageError(instance, err)
	}

	// Fetch route
	route, err := r.FetchRoute(instance)
	if err != nil {
		log.Error(err, errorRouteNotFound)
		return r.ManageError(instance, err)
	}

	// Route should point to TargetRef (Canary Weight 100)
	primaryService := &DestinationServiceDef{
		Name:   instance.Spec.TargetRef.Name,
		Weight: 100,
	}
	canaryService := &DestinationServiceDef{}
	if _, err := r.UpdateRouteDestinationsForCanary(route, primaryService, canaryService); err != nil {
		return r.ManageError(instance, err)
	}

	// Update Status with new primary
	instance.Status.IsCanaryRunning = false
	instance.Status.CanaryWeight = 0
	instance.Status.CanaryMetricValue = 0
	instance.Status.FailedChecks = 0
	instance.Status.ReleaseHistory = append(instance.Status.ReleaseHistory, kharonv1alpha1.Release{
		ID:   instance.Spec.TargetRef.Name,
		Name: instance.Spec.TargetRef.Name,
		Ref:  instance.Spec.TargetRef,
	})
	instance.Status.Iterations++
	instance.Status.LastStepTime = metav1.Time{}

	// Send notification event
	r.recorder.Eventf(instance, "Normal", string(kharonv1alpha1.EndCanaryRelease), "Canary release %s ended deployment %s with success", instance.ObjectMeta.Name, instance.Spec.TargetRef.Name)

	return r.ManageSuccess(instance, time.Duration(instance.Spec.CanaryAnalysis.Interval)*time.Second, kharonv1alpha1.EndCanaryRelease)
}

// CreateServiceForTargetRef creates a Service for Target
func (r *ReconcileCanary) CreateServiceForTargetRef(instance *kharonv1alpha1.Canary) (*corev1.Service, error) {
	// We have to check if there is a Service called as the TargetRef.Name, otherwise create it
	targetService := &corev1.Service{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.TargetRef.Name, Namespace: instance.Namespace}, targetService)
	if err != nil && errors.IsNotFound(err) {
		portName := instance.Spec.TargetRefContainerPort.StrVal
		if len(portName) <= 0 {
			portName = fmt.Sprintf("%d-%s", instance.Spec.TargetRefContainerPort.IntVal, strings.ToLower(string(instance.Spec.TargetRefContainerProtocol)))
		}
		// The Service we need should be named as the Deployment because exposes the Deployment logic (as a canary)
		targetServiceDef := &TargetServiceDef{
			serviceName: instance.Spec.TargetRef.Name,
			namespace:   instance.Namespace,
			selector:    instance.Spec.TargetRefSelector,
			portName:    portName,
			protocol:    instance.Spec.TargetRefContainerProtocol,
			port:        instance.Spec.TargetRefContainerPort.IntVal,
			targetPort:  instance.Spec.TargetRefContainerPort,
		}
		targetService = newServiceFromTargetServiceDef(targetServiceDef)
		// Set Canary instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, targetService, r.scheme); err != nil {
			return nil, err
		}
		log.Info("Creating the canary service", "CanaryService.Namespace", targetService.Namespace, "CanaryService.Name", targetService.Name)
		err = r.client.Create(context.TODO(), targetService)
		if err != nil && !errors.IsAlreadyExists(err) {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return targetService, nil
}

// CreateRouteForCanary creates a Route for Target
func (r *ReconcileCanary) CreateRouteForCanary(instance *kharonv1alpha1.Canary,
	primaryService *DestinationServiceDef,
	canaryService *DestinationServiceDef) (*routev1.Route, error) {
	// We have to check if there is a Route called canary.Spec.ServiceName, otherwise create it
	targetRoute := &routev1.Route{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.ServiceName, Namespace: instance.Namespace}, targetRoute)
	if err != nil && errors.IsNotFound(err) {
		// There's no route, so we have to create it from a route definition object (TargetRouteDef)
		// TargetRouteDef defines primary and canary services to route traffic to

		// The Route we need should be named as the Deployment because exposes the Deployment logic (as a canary)
		targetRouteDef := &TargetRouteDef{
			routeName:      instance.Spec.ServiceName,
			namespace:      instance.Namespace,
			selector:       instance.Spec.TargetRefSelector,
			targetPort:     instance.Spec.TargetRefContainerPort,
			primaryService: primaryService,
			canaryService:  canaryService,
		}
		targetRoute = newRouteFromTargetRouteDef(targetRouteDef)
		// Set Canary instance as the owner and controller
		if err := controllerutil.SetControllerReference(instance, targetRoute, r.scheme); err != nil {
			return nil, err
		}
		log.Info("Creating the canary route", "CanaryService.Namespace", targetRoute.Namespace, "CanaryService.Name", targetRoute.Name)
		err = r.client.Create(context.TODO(), targetRoute)
		if err != nil && !errors.IsAlreadyExists(err) {
			return nil, err
		}
		// No errors, so return created Route
		return targetRoute, nil
	} else if err != nil {
		return nil, err
	}

	// Let's update the route
	updateRouteDestinations(targetRoute, primaryService, canaryService)

	return targetRoute, nil
}

// UpdateRouteDestinationsForCanary updates a Route with new destinations
func (r *ReconcileCanary) UpdateRouteDestinationsForCanary(
	route *routev1.Route,
	primaryService *DestinationServiceDef,
	canaryService *DestinationServiceDef) (*routev1.Route, error) {
	if route == nil {
		return nil, _util.NewError("No route to be updated")
	}

	// Let's update the route
	updateRouteDestinations(route, primaryService, canaryService)
	if err := r.client.Update(context.TODO(), route); err != nil {
		return nil, err
	}

	return route, nil
}

// IsValid checks if our CR is valid or not
func (r *ReconcileCanary) IsValid(obj metav1.Object) (bool, error) {
	//log.Info(fmt.Sprintf("IsValid? %s", obj))

	canary, ok := obj.(*kharonv1alpha1.Canary)
	if !ok {
		err := errors.NewBadRequest(errorNotACanaryObject)
		log.Error(err, errorNotACanaryObject)
		return false, err
	}

	// Check if TargetRef is empty
	if (kharonv1alpha1.Ref{}) == canary.Spec.TargetRef {
		err := errors.NewBadRequest(errorTargetRefEmpty)
		log.Error(err, errorTargetRefEmpty)
		return false, err
	}

	// Check if TargetRefContainerPort is empty
	if len(canary.Spec.TargetRefContainerPort.StrVal) <= 0 && canary.Spec.TargetRefContainerPort.IntVal <= 0 {
		err := errors.NewBadRequest(errorTargetRefContainerPortEmpty)
		log.Error(err, errorTargetRefContainerPortEmpty)
		return false, err
	}

	// Check kind of target
	if canary.Spec.TargetRef.Kind != "Deployment" && canary.Spec.TargetRef.Kind != "DeploymentConfig" {
		err := errors.NewBadRequest(errorTargetRefKind)
		log.Error(err, errorTargetRefKind)
		return false, err
	}

	// Check if ServiceName is empty
	if len(canary.Spec.ServiceName) <= 0 {
		err := errors.NewBadRequest(errorServiceNameEmpty)
		log.Error(err, errorServiceNameEmpty)
		return false, err
	}

	// Check if CanaryAnalysis is empty
	if (kharonv1alpha1.CanaryAnalysis{}) == canary.Spec.CanaryAnalysis {
		err := errors.NewBadRequest(errorCanaryAnalysisEmpty)
		log.Error(err, errorCanaryAnalysisEmpty)
		return false, err
	}

	return true, nil
}

// ManageError manages an error object, an instance of the CR is passed along
func (r *ReconcileCanary) ManageError(obj metav1.Object, issue error) (reconcile.Result, error) {
	log.Error(issue, "Error managed")
	runtimeObj, ok := (obj).(runtime.Object)
	if !ok {
		log.Error(errors.NewBadRequest("not a runtime.Object"), "passed object was not a runtime.Object", "object", obj)
		return reconcile.Result{}, nil
	}
	var retryInterval time.Duration
	r.recorder.Event(runtimeObj, "Warning", "ProcessingError", issue.Error())
	if canary, ok := (obj).(*kharonv1alpha1.Canary); ok {
		lastUpdate := canary.Status.LastUpdate
		lastStatus := canary.Status.Status
		status := kharonv1alpha1.ReconcileStatus{
			LastUpdate: metav1.Now(),
			Reason:     issue.Error(),
			Status:     kharonv1alpha1.CanaryConditionStatusFailure,
		}
		canary.Status.ReconcileStatus = status
		err := r.client.Status().Update(context.Background(), runtimeObj)
		if err != nil {
			log.Error(err, errorUnableToUpdateStatus)
			return reconcile.Result{
				RequeueAfter: time.Second,
				Requeue:      true,
			}, nil
		}
		if lastUpdate.IsZero() || lastStatus == "Success" {
			retryInterval = time.Second
		} else {
			retryInterval = status.LastUpdate.Sub(lastUpdate.Time.Round(time.Second))
		}
	} else {
		log.Info("object is not RecocileStatusAware, not setting status")
		retryInterval = time.Second
	}
	return reconcile.Result{
		RequeueAfter: time.Duration(math.Min(float64(retryInterval.Nanoseconds()*2), float64(time.Hour.Nanoseconds()*6))),
		Requeue:      true,
	}, nil
}

// ManageSuccess manages a success and updates status accordingly, an instance of the CR is passed along
func (r *ReconcileCanary) ManageSuccess(obj metav1.Object, requeueAfter time.Duration, action kharonv1alpha1.ActionType) (reconcile.Result, error) {
	log.Info(fmt.Sprintf("===> ManageSuccess with requeueAfter: %d from: %s", requeueAfter, action))
	runtimeObj, ok := (obj).(runtime.Object)
	if !ok {
		log.Error(errors.NewBadRequest("not a runtime.Object"), "passed object was not a runtime.Object", "object", obj)
		return reconcile.Result{}, nil
	}
	if canary, ok := (obj).(*kharonv1alpha1.Canary); ok {
		status := kharonv1alpha1.ReconcileStatus{
			LastUpdate: metav1.Now(),
			Reason:     "",
			Status:     kharonv1alpha1.CanaryConditionStatusTrue,
		}
		canary.Status.ReconcileStatus = status
		canary.Status.LastAction = action

		err := r.client.Status().Update(context.Background(), runtimeObj)
		if err != nil {
			log.Error(err, "Unable to update status")
			r.recorder.Event(runtimeObj, "Warning", "ProcessingError", "Unable to update status")
			return reconcile.Result{
				RequeueAfter: time.Second,
				Requeue:      true,
			}, nil
		}
		//if canary.Status.IsCanaryRunning {
		//	r.recorder.Event(runtimeObj, "Normal", "StatusUpdate", fmt.Sprintf("Canary in progress %d%%", canary.Status.CanaryWeight))
		//}
	} else {
		log.Info("object is not Canary, not setting status")
		r.recorder.Event(runtimeObj, "Warning", "ProcessingError", "Object is not Canary, not setting status")
	}

	if requeueAfter > 0 {
		return reconcile.Result{
			RequeueAfter: requeueAfter,
			Requeue:      true,
		}, nil
	}
	return reconcile.Result{}, nil
}

// Creates a Service given a TargetServiceDef
func newServiceFromTargetServiceDef(targetServiceDef *TargetServiceDef) *corev1.Service {
	annotations := map[string]string{
		"openshift.io/generated-by": operatorName,
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        targetServiceDef.serviceName,
			Namespace:   targetServiceDef.namespace,
			Labels:      targetServiceDef.selector,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			SessionAffinity: corev1.ServiceAffinityNone,
			Selector:        targetServiceDef.selector,
			Ports: []corev1.ServicePort{
				{
					Name:       targetServiceDef.portName,
					Protocol:   targetServiceDef.protocol,
					Port:       targetServiceDef.port,
					TargetPort: targetServiceDef.targetPort,
				},
			},
		},
	}
}

// Creates a Route given a ...
func newRouteFromTargetRouteDef(targetRouteDef *TargetRouteDef) *routev1.Route {
	annotations := map[string]string{
		"openshift.io/generated-by": operatorName,
	}
	alternateBackends := []routev1.RouteTargetReference{}
	if len(targetRouteDef.canaryService.Name) > 0 {
		canaryWeight := 100 - targetRouteDef.primaryService.Weight
		alternateBackends = []routev1.RouteTargetReference{routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   targetRouteDef.canaryService.Name,
			Weight: &canaryWeight,
		}}
	}
	return &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:        targetRouteDef.routeName,
			Namespace:   targetRouteDef.namespace,
			Labels:      targetRouteDef.selector,
			Annotations: annotations,
		},
		Spec: routev1.RouteSpec{
			Port: &routev1.RoutePort{
				TargetPort: targetRouteDef.targetPort,
			},
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   targetRouteDef.primaryService.Name,
				Weight: &targetRouteDef.primaryService.Weight,
			},
			AlternateBackends: alternateBackends,
		},
	}
}

// Updates destinations of route
func updateRouteDestinations(route *routev1.Route,
	primaryService *DestinationServiceDef,
	canaryService *DestinationServiceDef) {
	route.Spec.To = routev1.RouteTargetReference{
		Kind:   "Service",
		Name:   primaryService.Name,
		Weight: &primaryService.Weight,
	}
	alternateBackends := []routev1.RouteTargetReference{}
	if canaryService != nil && (DestinationServiceDef{}) != *canaryService {
		canaryWeight := 100 - primaryService.Weight
		alternateBackends = []routev1.RouteTargetReference{routev1.RouteTargetReference{
			Kind:   "Service",
			Name:   canaryService.Name,
			Weight: &canaryWeight,
		}}
	}
	route.Spec.AlternateBackends = alternateBackends
}

// IsInitialized checks if our CR has been initialized or not
func (r *ReconcileCanary) IsInitialized(instance metav1.Object, target runtime.Object) (bool, error) {
	canary, ok := instance.(*kharonv1alpha1.Canary)
	if !ok {
		err := errors.NewBadRequest(errorNotACanaryObject)
		log.Error(err, errorNotACanaryObject)
		return false, err
	}
	if canary.Spec.Initialized {
		return true, nil
	}

	// Get containers from target, if no containers target is not valid
	containers := getContainersFromTarget(target)
	if len(containers) <= 0 {
		err := errors.NewBadRequest(errorTargetRefNotValid)
		log.Error(err, errorTargetRefNotValid)
		return false, err
	}

	// If no targetRefContainerName has been speficied... we'll get the first one from the target
	if canary.Spec.TargetRefContainerName == "" {
		canary.Spec.TargetRefContainerName = containers[0].Name
	}

	// Find the container by name, unless TargetRefContainerName was specified and wrong it won't be nil
	container := findContainerByName(canary.Spec.TargetRefContainerName, containers)
	if container == nil {
		err := errors.NewBadRequest(errorCanaryObjectNotValid)
		log.Error(err, errorCanaryObjectNotValid)
		return false, err
	}

	// If our container has no Ports... error
	if len(container.Ports) <= 0 {
		err := errors.NewBadRequest(errorTargetRefNotValid)
		log.Error(err, errorTargetRefNotValid)
		return false, err
	}

	// If no TargetRefContainerPort has been specified... we'll get it from the container
	if len(canary.Spec.TargetRefContainerPort.StrVal) <= 0 && canary.Spec.TargetRefContainerPort.IntVal <= 0 {
		log.Info(fmt.Sprintf("canary.Spec.TargetRefContainerPort empty!"))
		if len(container.Ports[0].Name) > 0 {
			canary.Spec.TargetRefContainerPort = intstr.FromString(container.Ports[0].Name)
		} else {
			canary.Spec.TargetRefContainerPort = intstr.FromInt(int(container.Ports[0].ContainerPort))
		}
	}

	// TODO findPortByNameOrNumber()

	// If no targetRefContainerProtocol has been specified... we'll get it from the container
	if len(canary.Spec.TargetRefContainerProtocol) <= 0 {
		canary.Spec.TargetRefContainerProtocol = container.Ports[0].Protocol
	}

	// Get selector from target, if no selector target is not valid
	selector := getSelectorFromTarget(target)
	if len(selector) <= 0 {
		err := errors.NewBadRequest(errorTargetRefNotValid)
		log.Error(err, errorTargetRefNotValid)
		return false, err
	}

	// If no selector has been specified... we'll get it from the target
	if len(canary.Spec.TargetRefSelector) <= 0 {
		canary.Spec.TargetRefSelector = selector
	}

	// TODO add a Finalizer...
	// util.AddFinalizer(mycrd, controllerName)
	canary.Spec.Initialized = true
	return false, nil
}

func findPortByName(name string, ports []corev1.ContainerPort) *corev1.ContainerPort {
	for _, port := range ports {
		if port.Name == name {
			return &port
		}
	}

	return nil
}

func findContainerByName(name string, containers []corev1.Container) *corev1.Container {
	for _, container := range containers {
		if container.Name == name {
			return &container
		}
	}

	return nil
}

func getContainersFromTarget(target runtime.Object) []corev1.Container {
	targetType := reflect.TypeOf(target)
	if match, _ := regexp.MatchString(".*\\.Deployment$", targetType.String()); match {
		return target.(*appsv1.Deployment).Spec.Template.Spec.Containers
	} else if match, _ := regexp.MatchString(".*\\.DeploymentConfig$", targetType.String()); match {
		return target.(*oappsv1.DeploymentConfig).Spec.Template.Spec.Containers
	} else {
		log.Info(fmt.Sprintf("targetType: %s CODE ERROR, TARGET TYPE NOT SUPPORTED!", targetType.Name()))
	}

	return []corev1.Container{}
}

func getSelectorFromTarget(target runtime.Object) map[string]string {
	targetType := reflect.TypeOf(target)
	if match, _ := regexp.MatchString(".*\\.Deployment$", targetType.String()); match {
		return target.(*appsv1.Deployment).Spec.Selector.MatchLabels
	} else if match, _ := regexp.MatchString(".*\\.DeploymentConfig$", targetType.String()); match {
		return target.(*oappsv1.DeploymentConfig).Spec.Selector
	} else {
		log.Info(fmt.Sprintf("targetType: %s CODE ERROR, TARGET TYPE NOT SUPPORTED!", targetType.Name()))
	}

	return map[string]string{}
}
