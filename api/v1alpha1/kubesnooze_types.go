package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SnoozeBehavior defines how kubesnooze adjusts workloads during sleep or wake.
type SnoozeBehavior struct {
	// Replicas is the desired replica count for Deployments/StatefulSets.
	Replicas *int32 `json:"replicas,omitempty"`
	// HPAMinReplicas is the desired minReplicas value for HPAs.
	HPAMinReplicas *int32 `json:"hpaMinReplicas,omitempty"`
	// SuspendCronJobs toggles CronJob suspension state.
	SuspendCronJobs *bool `json:"suspendCronJobs,omitempty"`
}

// KubeSnoozeSpec defines the desired state of KubeSnooze.
type KubeSnoozeSpec struct {
	// Selector targets workloads in the namespace.
	Selector metav1.LabelSelector `json:"selector"`
	// SleepCron defines when to apply sleep behavior.
	SleepCron string `json:"sleepCron"`
	// WakeCron defines when to apply wake behavior.
	WakeCron string `json:"wakeCron,omitempty"`
	// Timezone is the timezone used by CronJobs.
	Timezone string `json:"timezone,omitempty"`
	// RunnerImage is the image used by the runner pods.
	RunnerImage string `json:"runnerImage,omitempty"`
	// Sleep describes how to scale down workloads.
	Sleep SnoozeBehavior `json:"sleep"`
	// Wake describes how to scale up workloads.
	Wake SnoozeBehavior `json:"wake"`
}

// KubeSnoozeStatus defines the observed state of KubeSnooze.
type KubeSnoozeStatus struct {
	// ObservedGeneration is the last observed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// LastSleepTime is when the last sleep action ran.
	LastSleepTime *metav1.Time `json:"lastSleepTime,omitempty"`
	// LastWakeTime is when the last wake action ran.
	LastWakeTime *metav1.Time `json:"lastWakeTime,omitempty"`
	// Conditions represent the latest available observations.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// KubeSnooze is the Schema for the kubesnoozes API.
type KubeSnooze struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeSnoozeSpec   `json:"spec,omitempty"`
	Status KubeSnoozeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// KubeSnoozeList contains a list of KubeSnooze.
type KubeSnoozeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeSnooze `json:"items"`
}

func init() {
	// Register custom resources with the scheme.
	SchemeBuilder.Register(&KubeSnooze{}, &KubeSnoozeList{})
}
