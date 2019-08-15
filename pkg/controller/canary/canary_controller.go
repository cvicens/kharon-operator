package canary

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"time"

	//"errors"

	kharonv1alpha1 "github.com/redhat/kharon-operator/pkg/apis/kharon/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1 "k8s.io/api/apps/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
	record "k8s.io/client-go/tools/record"

	oappsv1 "github.com/openshift/api/apps/v1"
	routev1 "github.com/openshift/api/route/v1"
)

// Operator Name
const operatorName = "KharonOperator"

// Best practices
const controllerName = "canary_controller"

const ERROR_TARGET_REF_EMPTY = "Not a proper Canary object because TargetRef is empty"
const ERROR_TARGET_REF_KIND = "Not a proper Canary object because TargetRef is not Deployment or DeploymentConfig"
const ERROR_TARGET_REF_NOT_VALID = "Not a proper Canary object because TargetRef points to an invalid object"
const ERROR_NOT_A_CANARY_OBJECT = "Not a Canary object"
const ERROR_CANARY_OBJECT_NOT_VALID = "Not a valid Canary object"

var log = logf.Log.WithName("controller_canary")

type TargetServiceDef struct {
	serviceName string
	namespace   string
	selector    map[string]string
	portName    string
	protocol    corev1.Protocol
	port        int32
	targetPort  intstr.IntOrString
}

type DestinationServiceDef struct {
	Name   string
	Weight int32
}

type TargetRouteDef struct {
	routeName      string
	namespace      string
	selector       map[string]string
	targetPort     intstr.IntOrString
	primaryService *DestinationServiceDef
	canaryService  *DestinationServiceDef
}

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

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

	// Watch for changes to primary resource Canary
	err = c.Watch(&source.Kind{Type: &kharonv1alpha1.Canary{}}, &handler.EnqueueRequestForObject{})
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
		//return reconcile.Result{}, err
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
			log.Error(err, "unable to update instance", "instance", instance)
			return r.ManageError(instance, err)
		}
		return reconcile.Result{}, nil
	} else {
		if err != nil {
			return r.ManageError(instance, err)
		}
	}

	// Canary is inititialized, target is fine... cotainer, port... all OK
	// We have to check if there is a Service called as the Target, otherwise create it

	// Check if the primary service already exists
	targetService := &corev1.Service{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: targetRef.Name, Namespace: instance.Namespace}, targetService)
	if err != nil && errors.IsNotFound(err) {
		portName := instance.Spec.TargetRefContainerPort.StrVal
		if len(portName) <= 0 {
			portName = fmt.Sprintf("%d-%s", instance.Spec.TargetRefContainerPort.IntVal, strings.ToLower(string(instance.Spec.TargetRefContainerProtocol)))
		}
		// The Service we need should be named as the Deployment because exposes the Deployment logic (as a canary)
		targetServiceDef := &TargetServiceDef{
			serviceName: targetRef.Name,
			namespace:   instance.Namespace,
			selector:    instance.Spec.TargetRefSelector,
			portName:    portName,
			protocol:    instance.Spec.TargetRefContainerProtocol,
			port:        instance.Spec.TargetRefContainerPort.IntVal,
			targetPort:  instance.Spec.TargetRefContainerPort,
		}
		targetService = newServiceFromTargetServiceDef(targetServiceDef)
		reqLogger.Info("Creating the canary service", "CanaryService.Namespace", targetService.Namespace, "CanaryService.Name", targetService.Name)
		err = r.client.Create(context.TODO(), targetService)
		if err != nil {
			return r.ManageError(instance, err)
		}
	} else if err != nil {
		return r.ManageError(instance, err)
	}

	// We have to check if there is a Route called canary.Spec.ServiceName, otherwise create it
	targetRoute := &routev1.Route{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: instance.Spec.ServiceName, Namespace: instance.Namespace}, targetRoute)
	if err != nil && errors.IsNotFound(err) {
		portName := instance.Spec.TargetRefContainerPort.StrVal
		if len(portName) <= 0 {
			portName = fmt.Sprintf("%d-%s", instance.Spec.TargetRefContainerPort.IntVal, strings.ToLower(string(instance.Spec.TargetRefContainerProtocol)))
		}

		primaryService := &DestinationServiceDef{}
		canaryService := &DestinationServiceDef{}
		// If there's no a previous release in the release history
		if len(instance.Status.ReleaseHistory) <= 0 {
			primaryService = &DestinationServiceDef{
				Name:   targetService.Name,
				Weight: 100,
			}
		} else {
			currentRelease := instance.Status.ReleaseHistory[len(instance.Status.ReleaseHistory)-1]
			primaryService = &DestinationServiceDef{
				Name:   targetService.Name,
				Weight: 100,
			}
			canaryService = &DestinationServiceDef{
				Name:   currentRelease.Name,
				Weight: 0, // TODO this has to change
			}
		}

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
		reqLogger.Info("Creating the canary route", "CanaryService.Namespace", targetService.Namespace, "CanaryService.Name", targetService.Name)
		err = r.client.Create(context.TODO(), targetRoute)
		if err != nil {
			return r.ManageError(instance, err)
		}
	} else if err != nil {
		return r.ManageError(instance, err)
	}

	// DEFINE THIS FURTHER!
	// Make Route point to the primary release and the canary

	// Define a new Pod object
	pod := newPodForCR(instance)

	// Set Canary instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this Pod already exists
	found := &corev1.Pod{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: pod.Name, Namespace: pod.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Pod", "Pod.Namespace", pod.Namespace, "Pod.Name", pod.Name)
		err = r.client.Create(context.TODO(), pod)
		if err != nil {
			return reconcile.Result{}, err
		}

		// Pod created successfully - don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		return reconcile.Result{}, err
	}

	// Pod already exists - don't requeue
	reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	return reconcile.Result{}, nil
}

// newPodForCR returns a busybox pod with the same name/namespace as the cr
func newPodForCR(cr *kharonv1alpha1.Canary) *corev1.Pod {
	labels := map[string]string{
		"app": cr.Name,
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name + "-pod",
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
				},
			},
		},
	}
}

// IsValid checks if our CR is valid or not
func (r *ReconcileCanary) IsValid(obj metav1.Object) (bool, error) {
	//log.Info(fmt.Sprintf("IsValid? %s", obj))

	canary, ok := obj.(*kharonv1alpha1.Canary)
	if !ok {
		err := errors.NewBadRequest(ERROR_NOT_A_CANARY_OBJECT)
		log.Error(err, ERROR_NOT_A_CANARY_OBJECT)
		return false, err
	}

	// Check if TargetRef is empty
	if (kharonv1alpha1.Ref{}) == canary.Spec.TargetRef {
		err := errors.NewBadRequest(ERROR_TARGET_REF_EMPTY)
		log.Error(err, "TargetRef is empty")
		return false, err
	}

	// Check kind of target
	if canary.Spec.TargetRef.Kind != "Deployment" && canary.Spec.TargetRef.Kind != "DeploymentConfig" {
		err := errors.NewBadRequest(ERROR_TARGET_REF_EMPTY)
		log.Error(err, "TargetRef is the wrong kind")
		return false, err
	}

	// Check if ServiceName is empty
	if len(canary.Spec.ServiceName) <= 0 {
		err := errors.NewBadRequest(ERROR_CANARY_OBJECT_NOT_VALID)
		log.Error(err, "ServiceName cannot be empty")
		return false, err
	}

	return true, nil
}

// ManageError manages an error object, an instance of the CR is passed along
func (r *ReconcileCanary) ManageError(obj metav1.Object, issue error) (reconcile.Result, error) {
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
			Status:     "Failure",
		}
		canary.Status.ReconcileStatus = status
		err := r.client.Status().Update(context.Background(), runtimeObj)
		if err != nil {
			log.Error(err, "unable to update status")
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
func (r *ReconcileCanary) ManageSuccess(obj metav1.Object) (reconcile.Result, error) {
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

		err := r.client.Status().Update(context.Background(), runtimeObj)
		if err != nil {
			log.Error(err, "unable to update status")
			return reconcile.Result{
				RequeueAfter: time.Second,
				Requeue:      true,
			}, nil
		}
	} else {
		log.Info("object is not Canary, not setting status")
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

// IsInitialized checks if our CR has been initialized or not
func (r *ReconcileCanary) IsInitialized(instance metav1.Object, target runtime.Object) (bool, error) {
	canary, ok := instance.(*kharonv1alpha1.Canary)
	if !ok {
		err := errors.NewBadRequest(ERROR_NOT_A_CANARY_OBJECT)
		log.Error(err, ERROR_NOT_A_CANARY_OBJECT)
		return false, err
	}
	if canary.Spec.Initialized {
		return true, nil
	}

	// Get containers from target, if no containers target is not valid
	containers := getContainersFromTarget(target)
	if len(containers) <= 0 {
		err := errors.NewBadRequest(ERROR_TARGET_REF_NOT_VALID)
		log.Error(err, ERROR_TARGET_REF_NOT_VALID)
		return false, err
	}

	// If no targetRefContainerName has been speficied... we'll get the first one from the target
	if canary.Spec.TargetRefContainerName == "" {
		canary.Spec.TargetRefContainerName = containers[0].Name
	}

	// Find the container by name, unless TargetRefContainerName was specified and wrong it won't be nil
	container := findContainerByName(canary.Spec.TargetRefContainerName, containers)
	if container == nil {
		err := errors.NewBadRequest(ERROR_CANARY_OBJECT_NOT_VALID)
		log.Error(err, ERROR_CANARY_OBJECT_NOT_VALID)
		return false, err
	}

	// If our container has no Ports... error
	if len(container.Ports) <= 0 {
		err := errors.NewBadRequest(ERROR_TARGET_REF_NOT_VALID)
		log.Error(err, ERROR_TARGET_REF_NOT_VALID)
		return false, err
	}

	// If no TargetRefContainerPort has been specified... we'll get it from the container
	if len(canary.Spec.TargetRefContainerPort.StrVal) <= 0 || canary.Spec.TargetRefContainerPort.IntVal <= 0 {
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
		err := errors.NewBadRequest(ERROR_TARGET_REF_NOT_VALID)
		log.Error(err, ERROR_TARGET_REF_NOT_VALID)
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

	/*if strings.Contains(targetType.Name(), "v1.Deployment") {
		return target.(*appsv1.Deployment).Spec.Template.Spec.Containers
	} else if strings.Contains(targetType.Name(), "v1.DeploymentConfig") {
		return target.(*oappsv1.DeploymentConfig).Spec.Template.Spec.Containers
	} else {
		log.Info(fmt.Sprintf("targetType: %s CODE ERROR, TARGET TYPE NOT SUPPORTED!", targetType))
	}*/

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
