package canary

import (
	"context"
	"fmt"
	"math"
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
	record "k8s.io/client-go/tools/record"

	oappsv1 "github.com/openshift/api/apps/v1"
)

// Best practices
const controllerName = "canary_controller"

const ERROR_TARGET_REF_EMPTY = "Not a proper Canary object because TargetRef is empty"
const ERROR_TARGET_REF_KIND = "Not a proper Canary object because TargetRef is not Deployment or DeploymentConfig"
const ERROR_NOT_A_CANARY_OBJECT = "Not a Canary object"

var log = logf.Log.WithName("controller_canary")

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

	log.Info(fmt.Sprintf("==== target ==== %s", target))

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
	log.Info(fmt.Sprintf("IsValid? %s", obj))

	canary, ok := obj.(*kharonv1alpha1.Canary)
	if !ok {
		err := errors.NewBadRequest(ERROR_NOT_A_CANARY_OBJECT)
		log.Error(err, ERROR_NOT_A_CANARY_OBJECT)
		return false, err
	}

	if canary.Spec.TargetRef.Kind != "Deployment" && canary.Spec.TargetRef.Kind != "DeploymentConfig" {
		err := errors.NewBadRequest(ERROR_TARGET_REF_EMPTY)
		log.Error(err, ERROR_TARGET_REF_EMPTY)
		return false, err
	}

	// Check if targetRelease is empty
	if (kharonv1alpha1.Ref{}) == canary.Spec.TargetRef {
		err := errors.NewBadRequest(ERROR_TARGET_REF_EMPTY)
		log.Error(err, ERROR_TARGET_REF_EMPTY)
		return false, err
	}
	return true, nil
}

// ManageErrorSimple manages an error object, an instance of the CR is passed along
func (r *ReconcileCanary) ManageErrorSimple(obj metav1.Object, err error) (reconcile.Result, error) {
	_, ok := obj.(*kharonv1alpha1.Canary)
	if !ok {
		return reconcile.Result{}, errors.NewBadRequest("not a Canary object")
	}
	// TODO: Add logic to differentiate... remediate... enqueue...
	return reconcile.Result{}, err
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
			Status:     "Success",
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
