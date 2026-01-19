package controllers

import (
	"context"
	"fmt"

	kubesnoozev1alpha1 "kubesnooze/api/v1alpha1"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	defaultRunnerImage = "ghcr.io/kubesnooze/kubesnooze-runner:latest"
	runnerServiceAccountName = "kubesnooze-runner"
)

// KubeSnoozeReconciler reconciles a KubeSnooze object.
type KubeSnoozeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=kubesnooze.io,resources=kubesnoozes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kubesnooze.io,resources=kubesnoozes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kubesnooze.io,resources=kubesnoozes/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete

func (r *KubeSnoozeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var snooze kubesnoozev1alpha1.KubeSnooze
	if err := r.Get(ctx, req.NamespacedName, &snooze); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if snooze.Namespace == "kube-system" {
		logger.Info("kube-system is ignored by design", "name", snooze.Name)
		return ctrl.Result{}, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(&snooze.Spec.Selector)
	if err != nil {
		meta.SetStatusCondition(&snooze.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "InvalidSelector",
			Message: err.Error(),
		})
		_ = r.Status().Update(ctx, &snooze)
		return ctrl.Result{}, err
	}

	if err := r.ensureRBAC(ctx, &snooze); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ensureCronJob(ctx, &snooze, "sleep", snooze.Spec.SleepCron, selector); err != nil {
		return ctrl.Result{}, err
	}

	if snooze.Spec.WakeCron != "" {
		if err := r.ensureCronJob(ctx, &snooze, "wake", snooze.Spec.WakeCron, selector); err != nil {
			return ctrl.Result{}, err
		}
	}

	snooze.Status.ObservedGeneration = snooze.Generation
	meta.SetStatusCondition(&snooze.Status.Conditions, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Reconciled",
		Message: "CronJobs are configured",
	})
	if err := r.Status().Update(ctx, &snooze); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KubeSnoozeReconciler) ensureRBAC(ctx context.Context, snooze *kubesnoozev1alpha1.KubeSnooze) error {
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      runnerServiceAccountName,
			Namespace: snooze.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, serviceAccount, func() error {
		return controllerutil.SetControllerReference(snooze, serviceAccount, r.Scheme)
	}); err != nil {
		return err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubesnooze-runner",
			Namespace: snooze.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments", "statefulsets"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
			{
				APIGroups: []string{"autoscaling"},
				Resources: []string{"horizontalpodautoscalers"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"cronjobs"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
		}
		return controllerutil.SetControllerReference(snooze, role, r.Scheme)
	}); err != nil {
		return err
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubesnooze-runner",
			Namespace: snooze.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, roleBinding, func() error {
		roleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     role.Name,
		}
		roleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount.Name,
				Namespace: snooze.Namespace,
			},
		}
		return controllerutil.SetControllerReference(snooze, roleBinding, r.Scheme)
	}); err != nil {
		return err
	}

	return nil
}

func (r *KubeSnoozeReconciler) ensureCronJob(ctx context.Context, snooze *kubesnoozev1alpha1.KubeSnooze, action string, schedule string, selector labels.Selector) error {
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kubesnooze-%s-%s", snooze.Name, action),
			Namespace: snooze.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cronJob, func() error {
		labelsMap := map[string]string{
			"app.kubernetes.io/name": "kubesnooze",
			"app.kubernetes.io/part-of": "kubesnooze",
			"kubesnooze.io/name": snooze.Name,
			"kubesnooze.io/action": action,
		}
		cronJob.Labels = mergeLabels(cronJob.Labels, labelsMap)
		cronJob.Spec.Schedule = schedule
		if snooze.Spec.Timezone != "" {
			cronJob.Spec.TimeZone = ptr.To(snooze.Spec.Timezone)
		} else {
			cronJob.Spec.TimeZone = nil
		}
		cronJob.Spec.ConcurrencyPolicy = batchv1.ForbidConcurrent
		cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName = runnerServiceAccountName
		cronJob.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever

		image := snooze.Spec.RunnerImage
		if image == "" {
			image = defaultRunnerImage
		}

		env := []corev1.EnvVar{
			{Name: "KUBESNOOZE_ACTION", Value: action},
			{Name: "KUBESNOOZE_NAMESPACE", Value: snooze.Namespace},
			{Name: "KUBESNOOZE_LABEL_SELECTOR", Value: selector.String()},
			{Name: "KUBESNOOZE_SLEEP_REPLICAS", Value: int32String(snooze.Spec.Sleep.Replicas)},
			{Name: "KUBESNOOZE_WAKE_REPLICAS", Value: int32String(snooze.Spec.Wake.Replicas)},
			{Name: "KUBESNOOZE_SLEEP_HPA_MIN_REPLICAS", Value: int32String(snooze.Spec.Sleep.HPAMinReplicas)},
			{Name: "KUBESNOOZE_WAKE_HPA_MIN_REPLICAS", Value: int32String(snooze.Spec.Wake.HPAMinReplicas)},
			{Name: "KUBESNOOZE_SLEEP_SUSPEND_CRONJOBS", Value: boolString(snooze.Spec.Sleep.SuspendCronJobs, true)},
			{Name: "KUBESNOOZE_WAKE_SUSPEND_CRONJOBS", Value: boolString(snooze.Spec.Wake.SuspendCronJobs, false)},
		}

		cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:  "kubesnooze-runner",
				Image: image,
				Env:   env,
			},
		}
		return controllerutil.SetControllerReference(snooze, cronJob, r.Scheme)
	})
	return err
}

func int32String(value *int32) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func boolString(value *bool, defaultValue bool) string {
	if value == nil {
		return fmt.Sprintf("%t", defaultValue)
	}
	return fmt.Sprintf("%t", *value)
}

func mergeLabels(existing map[string]string, add map[string]string) map[string]string {
	if existing == nil {
		existing = map[string]string{}
	}
	for key, value := range add {
		existing[key] = value
	}
	return existing
}

func (r *KubeSnoozeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubesnoozev1alpha1.KubeSnooze{}).
		Owns(&batchv1.CronJob{}).
		Complete(r)
}
