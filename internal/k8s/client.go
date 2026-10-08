package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Options — параметры создания клиента.
type Options struct {
	Kubeconfig    string
	Namespace     string
	PodPrefix     string
	PrometheusURL string
	// MetricsSource: "auto" | "metrics-server" | "prometheus".
	MetricsSource string
}

// Client — обёртка над официальными клиентами Kubernetes.
type Client struct {
	Kube      kubernetes.Interface
	Metrics   metricsv.Interface
	Discovery discovery.DiscoveryInterface
	Namespace string
	PodPrefix string

	prometheusURL string
	metricsSource string

	mu              sync.Mutex
	sources         []MetricsSource
	sourcesResolved bool
	sourceErr       error
}

// NewClient создаёт клиент. Если kubeconfig пуст, пробуем in-cluster,
// затем ~/.kube/config.
func NewClient(opts Options) (*Client, error) {
	restCfg, err := buildRestConfig(opts.Kubeconfig)
	if err != nil {
		return nil, err
	}

	kube, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	metrics, err := metricsv.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create metrics client: %w", err)
	}
	disc, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}

	src := opts.MetricsSource
	if src == "" {
		src = "auto"
	}
	return &Client{
		Kube:          kube,
		Metrics:       metrics,
		Discovery:     disc,
		Namespace:     opts.Namespace,
		PodPrefix:     opts.PodPrefix,
		prometheusURL: strings.TrimRight(opts.PrometheusURL, "/"),
		metricsSource: src,
	}, nil
}

func buildRestConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	// 1) in-cluster (сервис работает внутри кластера)
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	// 2) локальный kubeconfig
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".kube", "config"),
		clientcmd.RecommendedHomeFile,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			cfg, err := clientcmd.BuildConfigFromFlags("", p)
			if err != nil {
				return nil, fmt.Errorf("build config from %s: %w", p, err)
			}
			return cfg, nil
		}
	}
	return nil, fmt.Errorf("no kubernetes configuration found (set kubernetes.kubeconfig or run in-cluster)")
}

// ctx возвращает контекст с таймаутом для запросов к API.
func ctx() context.Context {
	return context.Background()
}
