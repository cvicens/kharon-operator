package v1alpha1

import (
	time "time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Name            string `json:"name"`
	Threshold       int64  `json:"threshold"`
	Interval        int64  `json:"interval"`
	PrometheusQuery string `json:"prometheusQuery"`
}

// CanaryAnalisys defines how to run analysis on a canary release
type CanaryAnalisys struct {
	MetricsServer string   `json:"metricsServer"`
	Interval      int64    `json:"interval"`
	Threshold     int64    `json:"threshold"`
	MaxWeight     int64    `json:"maxWeight"`
	StepWeight    int64    `json:"stepWeight"`
	Metrics       []Metric `json:"metrics"`
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
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html

	Enabled bool `json:"enabled"`
	// +kubebuilder:validation:Enum=Native,Istio
	Type           CanaryType     `json:"type"`
	TargetRef      Ref            `json:"targetRef"`
	CanaryAnalisys CanaryAnalisys `json:"canaryAnalisys"`
}

// CanaryConditionType defines the potential condition types
type CanaryConditionType string

const (
	Promoted    CanaryConditionType = "Succeded"
	Progressing CanaryConditionType = "Progressing"
	Failed      CanaryConditionType = "Failed"
)

// ConditionStatus defines the potential status
type ConditionStatus string

const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

// CanaryCondition defines the desired state of Canary
type CanaryCondition struct {
	// Type of replication controller condition.
	// +kubebuilder:validation:Enum=Succeded,Progressing,Failed
	Type CanaryConditionType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=CanaryConditionType"`
	// Status of the condition, one of True, False, Unknown.
	// +kubebuilder:validation:Enum=True,False,Unknown
	Status ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=ConditionStatus"`
	// The last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,3,opt,name=lastTransitionTime"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,4,opt,name=reason"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,5,opt,name=message"`
}

type ReconcileStatus struct {
	// +kubebuilder:validation:Enum=Success,Failure
	Status     string      `json:"status,omitempty"`
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`
	Reason     string      `json:"reason,omitempty"`
}

// CanaryStatus defines the observed state of Canary
// +k8s:openapi-gen=true
type CanaryStatus struct {
	ReconcileStatus `json:",inline"`

	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
	CanaryWeight     int64         `json:"canaryWeight"`
	FailedChecks     int64         `json:"failedChecks"`
	Iterations       int64         `json:"iterations"`
	LastAppliedSpec  time.Duration `json:"lastAppliedSpec"`
	LastPromotedSpec time.Duration `json:"lastPromotedSpec"`

	// Conditions used to wait like in => kubectl wait canary/podinfo --for=condition=promoted
	Conditions []CanaryCondition

	// Fed by the carany release process
	ReleaseHistory []Release `json:"releaseHistory,omitempty"`
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
