package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	annotationOriginalReplicas = "kubesnooze.io/original-replicas"
	annotationOriginalHPAMin   = "kubesnooze.io/original-hpa-min-replicas"
	envNamespace               = "KUBESNOOZE_NAMESPACE"
	envLabelSelector           = "KUBESNOOZE_LABEL_SELECTOR"
	envWakeReplicas            = "KUBESNOOZE_WAKE_REPLICAS"
	envWakeHPAMin              = "KUBESNOOZE_WAKE_HPA_MIN_REPLICAS"
	envPort                    = "KUBESNOOZE_PORT"
	envTitle                   = "KUBESNOOZE_TITLE"
	envMessage                 = "KUBESNOOZE_MESSAGE"
)

type splashConfig struct {
	namespace    string
	selector     labels.Selector
	wakeReplicas *int32
	wakeHPAMin   *int32
	port         string
	title        string
	message      string
}

type wakeService struct {
	clientset  *kubernetes.Clientset
	config     *splashConfig
	mu         sync.Mutex
	lastWakeAt time.Time
}

func main() {
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

	service := &wakeService{
		clientset: clientset,
		config:    config,
	}

	server := &http.Server{
		Addr:         ":" + config.port,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      service.routes(),
	}

	fmt.Printf("kubesnooze splash listening on %s\n", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fail(err)
	}
}

func (s *wakeService) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", s.handleSplash)
	return mux
}

func (s *wakeService) handleSplash(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	err := s.wake(ctx)
	data := map[string]string{
		"Title":   s.config.title,
		"Message": s.config.message,
	}
	if err != nil {
		data["Message"] = fmt.Sprintf("%s (wake failed: %v)", s.config.message, err)
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if tplErr := splashTemplate.Execute(w, data); tplErr != nil {
		fmt.Fprintf(os.Stderr, "render error: %v\n", tplErr)
	}
}

func (s *wakeService) wake(ctx context.Context) error {
	s.mu.Lock()
	if time.Since(s.lastWakeAt) < 10*time.Second {
		s.mu.Unlock()
		return nil
	}
	s.lastWakeAt = time.Now()
	s.mu.Unlock()

	if err := processDeployments(ctx, s.clientset, s.config); err != nil {
		return err
	}
	if err := processStatefulSets(ctx, s.clientset, s.config); err != nil {
		return err
	}
	if err := processHPAs(ctx, s.clientset, s.config); err != nil {
		return err
	}
	return nil
}

func loadConfig() (*splashConfig, error) {
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

	wakeReplicas := parseInt32Pointer(os.Getenv(envWakeReplicas))
	wakeHPAMin := parseInt32Pointer(os.Getenv(envWakeHPAMin))

	port := os.Getenv(envPort)
	if port == "" {
		port = "8080"
	}
	title := os.Getenv(envTitle)
	if title == "" {
		title = "KubeSnooze"
	}
	message := os.Getenv(envMessage)
	if message == "" {
		message = "Waking up this environment..."
	}

	return &splashConfig{
		namespace:    namespace,
		selector:     selector,
		wakeReplicas: wakeReplicas,
		wakeHPAMin:   wakeHPAMin,
		port:         port,
		title:        title,
		message:      message,
	}, nil
}

func processDeployments(ctx context.Context, clientset *kubernetes.Clientset, cfg *splashConfig) error {
	list, err := clientset.AppsV1().Deployments(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.selector.String(),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		deployment := &list.Items[i]
		if err := wakeDeployment(ctx, clientset, cfg, deployment); err != nil {
			return err
		}
	}
	return nil
}

func processStatefulSets(ctx context.Context, clientset *kubernetes.Clientset, cfg *splashConfig) error {
	list, err := clientset.AppsV1().StatefulSets(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.selector.String(),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		statefulset := &list.Items[i]
		if err := wakeStatefulSet(ctx, clientset, cfg, statefulset); err != nil {
			return err
		}
	}
	return nil
}

func processHPAs(ctx context.Context, clientset *kubernetes.Clientset, cfg *splashConfig) error {
	list, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: cfg.selector.String(),
	})
	if err != nil {
		return err
	}
	for i := range list.Items {
		hpa := &list.Items[i]
		if err := wakeHPAMinReplicas(ctx, clientset, cfg, hpa); err != nil {
			return err
		}
	}
	return nil
}

func wakeDeployment(ctx context.Context, clientset *kubernetes.Clientset, cfg *splashConfig, deployment *appsv1.Deployment) error {
	if deployment.Annotations == nil {
		deployment.Annotations = map[string]string{}
	}

	target := cfg.wakeReplicas
	if target == nil {
		if raw, ok := deployment.Annotations[annotationOriginalReplicas]; ok {
			parsed, err := strconv.Atoi(raw)
			if err == nil {
				value := int32(parsed)
				target = &value
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

func wakeStatefulSet(ctx context.Context, clientset *kubernetes.Clientset, cfg *splashConfig, statefulset *appsv1.StatefulSet) error {
	if statefulset.Annotations == nil {
		statefulset.Annotations = map[string]string{}
	}

	target := cfg.wakeReplicas
	if target == nil {
		if raw, ok := statefulset.Annotations[annotationOriginalReplicas]; ok {
			parsed, err := strconv.Atoi(raw)
			if err == nil {
				value := int32(parsed)
				target = &value
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

func wakeHPAMinReplicas(ctx context.Context, clientset *kubernetes.Clientset, cfg *splashConfig, hpa *autoscalingv2.HorizontalPodAutoscaler) error {
	if hpa.Annotations == nil {
		hpa.Annotations = map[string]string{}
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

func fail(err error) {
	fmt.Fprintf(os.Stderr, "kubesnooze splash error: %v\n", err)
	os.Exit(1)
}

var splashTemplate = template.Must(template.New("splash").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{ .Title }}</title>
  <style>
    body {
      margin: 0;
      padding: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: #0b1220;
      color: #f8fafc;
    }
    .wrap {
      min-height: 100vh;
      display: flex;
      align-items: center;
      justify-content: center;
      padding: 2rem;
    }
    .card {
      max-width: 560px;
      width: 100%;
      background: #111827;
      border-radius: 16px;
      padding: 32px;
      box-shadow: 0 12px 30px rgba(0, 0, 0, 0.35);
    }
    h1 {
      margin: 0 0 12px 0;
      font-size: 28px;
      font-weight: 700;
    }
    p {
      margin: 0;
      font-size: 16px;
      line-height: 1.6;
      color: #cbd5f5;
    }
    .hint {
      margin-top: 20px;
      font-size: 13px;
      color: #94a3b8;
    }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="card">
      <h1>{{ .Title }}</h1>
      <p>{{ .Message }}</p>
      <p class="hint">This page will refresh automatically.</p>
    </div>
  </div>
  <script>
    setTimeout(function () {
      window.location.reload();
    }, 12000);
  </script>
</body>
</html>`))
