package v1alpha1

import (
	time "time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	intstr "k8s.io/apimachinery/pkg/util/intstr"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Ref defines a pointer to Deployment, DeploymentConfig, ...
type Ref struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// Release defines a pointer to a Deployment, DeploymentConfig, ... we want to promote
type Release struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Ref  Ref
}

// Metric defines a metric towards we check if the canary is fine
type Metric struct {
	Name            string  `json:"name"`
	Threshold       float64 `json:"threshold"`
	Interval        int32   `json:"interval"`
	PrometheusQuery string  `json:"prometheusQuery"`
}

// CanaryAnalisys defines how to run analysis on a canary release
type CanaryAnalysis struct {
	MetricsServer string  `json:"metricsServer"`
	Interval      int32   `json:"interval"` // In seconds
	Threshold     float32 `json:"threshold"`
	MaxWeight     int32   `json:"maxWeight"`
	StepWeight    int32   `json:"stepWeight"`
	Metric        Metric  `json:"metric"`
}

// CanaryType defines the potential condition types
type CanaryType string

const (
	Native CanaryType = "Native"
	Istio  CanaryType = "Istio"
)

// CanarySpec defines the desired state of Canary
// +k8s:openapi-gen=true
type CanarySpec struct {
	// Flags if the Canary object is enabled or not
	Enabled bool `json:"enabled"`
	// Flags if Canary has been initialized or not
	Initialized bool `json:"initialized"`
	// Two types of Canary releases, Native or Istio
	// +kubebuilder:validation:Enum=Native,Istio
	Type CanaryType `json:"type"`
	// Name of the primary service, will be used to create a Service and Route or VirtualService
	ServiceName string `json:"serviceName"`
	// Reference to the Deployment or DeploymentConfig from which to generate the Canary release
	TargetRef Ref `json:"targetRef"`
	// Selector, if empty take the labels of the template of the Target Deployment
	TargetRefSelector map[string]string `json:"targetRefSelector"`
	// Name of the container in the Deployment, if empty take the first one
	TargetRefContainerName string `json:"targetRefContainerName"`
	// Name of the port in the container in the Deployment, if empty take the first one
	TargetRefContainerPort intstr.IntOrString `json:"targetRefContainerPort"`
	// Protocol of container in the Deployment, if empty take the first one
	TargetRefContainerProtocol corev1.Protocol `json:"targetRefContainerProtocol"`
	// Canary Analisys Settings
	CanaryAnalysis CanaryAnalysis `json:"canaryAnalysis"`
}

// CanaryConditionType defines the potential condition types
type CanaryConditionType string

const (
	CanaryConditionTypePromoted CanaryConditionType = "Promoted"
)

// CanaryConditionReason defines the potential condition reasons
type CanaryConditionReason string

const (
	CanaryConditionReasonInitialized CanaryConditionReason = "Initialized"
	CanaryConditionReasonWaiting     CanaryConditionReason = "Waiting"
	CanaryConditionReasonProgressing CanaryConditionReason = "Progressing"
	CanaryConditionReasonFinalising  CanaryConditionReason = "Finalising"
	CanaryConditionReasonSucceeded   CanaryConditionReason = "Succeeded"
	CanaryConditionReasonFailed      CanaryConditionReason = "Failed"
)

// ConditionStatus defines the potential status
type CanaryConditionStatus string

const (
	CanaryConditionStatusTrue    CanaryConditionStatus = "True"
	CanaryConditionStatusFalse   CanaryConditionStatus = "False"
	CanaryConditionStatusUnknown CanaryConditionStatus = "Unknown"
)

// CanaryCondition defines the desired state of Canary
type CanaryCondition struct {
	// Type of replication controller condition.
	// +kubebuilder:validation:Enum=Promoted
	Type CanaryConditionType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=CanaryConditionType"`
	// Status of the condition, one of True, False, Unknown.
	// +kubebuilder:validation:Enum=True,False,Unknown
	Status CanaryConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=ConditionStatus"`
	// The last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,3,opt,name=lastTransitionTime"`
	// The reason for the condition's last transition.
	// +optional
	// +kubebuilder:validation:Enum=Initialized,Waiting,Progressing,Finalising,Succeeded,Failed
	Reason CanaryConditionReason `json:"reason,omitempty" protobuf:"bytes,4,opt,name=reason"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,5,opt,name=message"`
}

type ReconcileStatus struct {
	// +kubebuilder:validation:Enum=Succeded,Progressing,Failed
	Status     CanaryConditionStatus `json:"status,omitempty"`
	LastUpdate metav1.Time           `json:"lastUpdate,omitempty"`
	Reason     string                `json:"reason,omitempty"`
}

// CanaryStatus defines the observed state of Canary
// +k8s:openapi-gen=true
type CanaryStatus struct {
	ReconcileStatus `json:",inline"`

	IsCanaryRunning   bool              `json:"isCanaryRunning"`
	CanaryWeight      int32             `json:"canaryWeight"`
	CanaryMetricValue float64           `json:"canaryMetricValue"`
	FailedChecks      int64             `json:"failedChecks"`
	Iterations        int64             `json:"iterations"`
	LastAppliedSpec   time.Duration     `json:"lastAppliedSpec"`
	LastPromotedSpec  time.Duration     `json:"lastPromotedSpec"`
	LastStepTime      metav1.Time       `json:"lastStepTime"`
	Conditions        []CanaryCondition `json:"conditions,omitempty"`     // Used to wait => kubectl wait canary/podinfo --for=condition=promoted
	ReleaseHistory    []Release         `json:"releaseHistory,omitempty"` // Fed by the carany release process
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Canary is the Schema for the canaries API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type Canary struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CanarySpec   `json:"spec,omitempty"`
	Status CanaryStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CanaryList contains a list of Canary
type CanaryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Canary `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Canary{}, &CanaryList{})
}
