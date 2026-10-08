package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"opencode-resources/internal/api"
	"opencode-resources/internal/config"
	"opencode-resources/internal/k8s"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "путь к файлу конфигурации")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("загрузка конфигурации: %v", err)
	}

	client, err := k8s.NewClient(k8s.Options{
		Kubeconfig:    cfg.Kubernetes.Kubeconfig,
		Namespace:     cfg.Kubernetes.Namespace,
		PodPrefix:     cfg.Kubernetes.PodPrefix,
		PrometheusURL: cfg.Metrics.PrometheusURL,
		MetricsSource: cfg.Metrics.Source,
		AuthMode:      cfg.Kubernetes.AuthMode,
	})
	if err != nil {
		log.Fatalf("инициализация k8s клиента: %v", err)
	}

	srv := api.NewServer(client)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("сервис запущен: http://%s (namespace=%s, prefix=%s)", addr, cfg.Kubernetes.Namespace, cfg.Kubernetes.PodPrefix)
	if err := http.ListenAndServe(addr, srv.Routes()); err != nil {
		log.Fatalf("ошибка сервера: %v", err)
	}
}
