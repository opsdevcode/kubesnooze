package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	annotationOriginalReplicas    = "kubesnooze.io/original-replicas"
	annotationOriginalHPAMin      = "kubesnooze.io/original-hpa-min-replicas"
	envAction                     = "KUBESNOOZE_ACTION"
	envNamespace                  = "KUBESNOOZE_NAMESPACE"
	envLabelSelector              = "KUBESNOOZE_LABEL_SELECTOR"
	envSleepReplicas              = "KUBESNOOZE_SLEEP_REPLICAS"
	envWakeReplicas               = "KUBESNOOZE_WAKE_REPLICAS"
	envSleepHPAMin                = "KUBESNOOZE_SLEEP_HPA_MIN_REPLICAS"
	envWakeHPAMin                 = "KUBESNOOZE_WAKE_HPA_MIN_REPLICAS"
	envSleepSuspendCronJobs        = "KUBESNOOZE_SLEEP_SUSPEND_CRONJOBS"
	envWakeSuspendCronJobs         = "KUBESNOOZE_WAKE_SUSPEND_CRONJOBS"
)

type runnerConfig struct {
	action            string
	namespace         string
	selector          labels.Selector
	sleepReplicas     *int32
	wakeReplicas      *int32
	sleepHPAMin       *int32
	wakeHPAMin        *int32
	sleepSuspendCron  bool
	wakeSuspendCron   bool
}

func main() {
	ctx := context.Background()
	config, err := loadConfig()
	if err != nil {
		fail(err)
	}

	if config.namespace == "kube-system" {
		fmt.Println("kube-system is ignored by design")
		return
	}

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		fail(err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		fail(err)
	}

	if err := processDeployments(ctx, clientset, config); err != nil {
		fail(err)
	}
	if err := processStatefulSets(ctx, clientset, config); err != nil {
		fail(err)
	}
	if err := processHPAs(ctx, clientset, config); err != nil {
		fail(err)
	}
	if err := processCronJobs(ctx, clientset, config); err != nil {
		fail(err)
	}
}

func loadConfig() (*runnerConfig, error) {
	action := os.Getenv(envAction)
	if action != "sleep" && action != "wake" {
		return nil, fmt.Errorf("invalid action: %q", action)
	}
	namespace := os.Getenv(envNamespace)
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	selectorRaw := os.Getenv(envLabelSelector)
	if selectorRaw == "" {
		return nil, fmt.Errorf("label selector is required")
	}
	selector, err := labels.Parse(selectorRaw)
	if err != nil {
		return nil, err
	}

	sleepReplicas := parseInt32Pointer(os.Getenv(envSleepReplicas))
	wakeReplicas := parseInt32Pointer(os.Getenv(envWakeReplicas))
	sleepHPAMin := parseInt32Pointer(os.Getenv(envSleepHPAMin))
	wakeHPAMin := parseInt32Pointer(os.Getenv(envWakeHPAMin))
	sleepSuspendCron := parseBoolDefault(os.Getenv(envSleepSuspendCronJobs), true)
	wakeSuspendCron := parseBoolDefault(os.Getenv(envWakeSuspendCronJobs), false)

	return &runnerConfig{
		action:          action,
		namespace:       namespace,
		selector:        selector,
		sleepReplicas:   sleepReplicas,
		wakeReplicas:    wakeReplicas,
		sleepHPAMin:     sleepHPAMin,
		wakeHPAMin:      wakeHPAMin,
		sleepSuspendCron: sleepSuspendCron,
		wakeSuspendCron:  wakeSuspendCron,
	}, nil
}

func processDeployments(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig) error {
	list, err := clientset.AppsV1().Deployments(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.selector.String(),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		deployment := &list.Items[i]
		if err := updateWorkloadReplicas(ctx, clientset, cfg, deployment); err != nil {
			return err
		}
	}
	return nil
}

func processStatefulSets(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig) error {
	list, err := clientset.AppsV1().StatefulSets(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.selector.String(),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		statefulset := &list.Items[i]
		if err := updateWorkloadReplicas(ctx, clientset, cfg, statefulset); err != nil {
			return err
		}
	}
	return nil
}

func processHPAs(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig) error {
	list, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.selector.String(),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		hpa := &list.Items[i]
		if err := updateHPAMinReplicas(ctx, clientset, cfg, hpa); err != nil {
			return err
		}
	}
	return nil
}

func processCronJobs(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig) error {
	list, err := clientset.BatchV1().CronJobs(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.selector.String(),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		cronJob := &list.Items[i]
		if err := updateCronJobSuspension(ctx, clientset, cfg, cronJob); err != nil {
			return err
		}
	}
	return nil
}

func updateWorkloadReplicas(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig, workload metav1.Object) error {
	switch obj := workload.(type) {
	case *appsv1.Deployment:
		return updateDeployment(ctx, clientset, cfg, obj)
	case *appsv1.StatefulSet:
		return updateStatefulSet(ctx, clientset, cfg, obj)
	default:
		return fmt.Errorf("unsupported workload type %T", workload)
	}
}

func updateDeployment(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig, deployment *appsv1.Deployment) error {
	replicas := deployment.Spec.Replicas
	if replicas == nil {
		replicas = int32Ptr(1)
	}
	if deployment.Annotations == nil {
		deployment.Annotations = map[string]string{}
	}

	if cfg.action == "sleep" {
		if _, ok := deployment.Annotations[annotationOriginalReplicas]; !ok {
			deployment.Annotations[annotationOriginalReplicas] = fmt.Sprintf("%d", *replicas)
		}
		target := int32Ptr(defaultInt32(cfg.sleepReplicas, 0))
		deployment.Spec.Replicas = target
		_, err := clientset.AppsV1().Deployments(cfg.namespace).Update(ctx, deployment, metav1.UpdateOptions{})
		return err
	}

	target := cfg.wakeReplicas
	if target == nil {
		if raw, ok := deployment.Annotations[annotationOriginalReplicas]; ok {
			parsed, err := strconv.Atoi(raw)
			if err == nil {
				target = int32Ptr(int32(parsed))
			}
		}
	}
	if target != nil {
		deployment.Spec.Replicas = target
		_, err := clientset.AppsV1().Deployments(cfg.namespace).Update(ctx, deployment, metav1.UpdateOptions{})
		return err
	}
	return nil
}

func updateStatefulSet(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig, statefulset *appsv1.StatefulSet) error {
	replicas := statefulset.Spec.Replicas
	if replicas == nil {
		replicas = int32Ptr(1)
	}
	if statefulset.Annotations == nil {
		statefulset.Annotations = map[string]string{}
	}

	if cfg.action == "sleep" {
		if _, ok := statefulset.Annotations[annotationOriginalReplicas]; !ok {
			statefulset.Annotations[annotationOriginalReplicas] = fmt.Sprintf("%d", *replicas)
		}
		target := int32Ptr(defaultInt32(cfg.sleepReplicas, 0))
		statefulset.Spec.Replicas = target
		_, err := clientset.AppsV1().StatefulSets(cfg.namespace).Update(ctx, statefulset, metav1.UpdateOptions{})
		return err
	}

	target := cfg.wakeReplicas
	if target == nil {
		if raw, ok := statefulset.Annotations[annotationOriginalReplicas]; ok {
			parsed, err := strconv.Atoi(raw)
			if err == nil {
				target = int32Ptr(int32(parsed))
			}
		}
	}
	if target != nil {
		statefulset.Spec.Replicas = target
		_, err := clientset.AppsV1().StatefulSets(cfg.namespace).Update(ctx, statefulset, metav1.UpdateOptions{})
		return err
	}
	return nil
}

func updateHPAMinReplicas(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig, hpa *autoscalingv2.HorizontalPodAutoscaler) error {
	if hpa.Annotations == nil {
		hpa.Annotations = map[string]string{}
	}

	if cfg.action == "sleep" {
		if hpa.Spec.MinReplicas != nil {
			if _, ok := hpa.Annotations[annotationOriginalHPAMin]; !ok {
				hpa.Annotations[annotationOriginalHPAMin] = fmt.Sprintf("%d", *hpa.Spec.MinReplicas)
			}
		}
		target := defaultInt32(cfg.sleepHPAMin, 1)
		hpa.Spec.MinReplicas = int32Ptr(target)
		_, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(cfg.namespace).Update(ctx, hpa, metav1.UpdateOptions{})
		return err
	}

	target := cfg.wakeHPAMin
	if target == nil {
		if raw, ok := hpa.Annotations[annotationOriginalHPAMin]; ok {
			parsed, err := strconv.Atoi(raw)
			if err == nil {
				value := int32(parsed)
				target = &value
			}
		}
	}
	if target != nil {
		hpa.Spec.MinReplicas = target
		_, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(cfg.namespace).Update(ctx, hpa, metav1.UpdateOptions{})
		return err
	}
	return nil
}

func updateCronJobSuspension(ctx context.Context, clientset *kubernetes.Clientset, cfg *runnerConfig, cronJob *batchv1.CronJob) error {
	var suspend bool
	if cfg.action == "sleep" {
		suspend = cfg.sleepSuspendCron
	} else {
		suspend = cfg.wakeSuspendCron
	}
	cronJob.Spec.Suspend = &suspend
	_, err := clientset.BatchV1().CronJobs(cfg.namespace).Update(ctx, cronJob, metav1.UpdateOptions{})
	return err
}

func parseInt32Pointer(value string) *int32 {
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	result := int32(parsed)
	return &result
}

func parseBoolDefault(value string, defaultValue bool) bool {
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

func defaultInt32(value *int32, defaultValue int32) int32 {
	if value == nil {
		return defaultValue
	}
	return *value
}

func int32Ptr(value int32) *int32 {
	return &value
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "kubesnooze runner error: %v\n", err)
	os.Exit(1)
}
