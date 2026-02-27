package v1alpha1

import (
	"testing"
)

func TestKubeSnoozeSpec_Defaults(t *testing.T) {
	spec := KubeSnoozeSpec{
		SleepCron: "0 22 * * 1-5",
		WakeCron:  "0 6 * * 1-5",
	}
	if spec.SleepCron == "" {
		t.Error("SleepCron should not be empty")
	}
	if spec.WakeCron == "" {
		t.Error("WakeCron should not be empty")
	}
}

func TestSnoozeBehavior_Replicas(t *testing.T) {
	zero := int32(0)
	one := int32(1)
	sleep := SnoozeBehavior{Replicas: &zero}
	wake := SnoozeBehavior{Replicas: &one}
	if sleep.Replicas == nil || *sleep.Replicas != 0 {
		t.Error("sleep Replicas should be 0")
	}
	if wake.Replicas == nil || *wake.Replicas != 1 {
		t.Error("wake Replicas should be 1")
	}
}
