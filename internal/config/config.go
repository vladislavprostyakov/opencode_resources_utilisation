package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config — корневая структура конфигурации.
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	Kubernetes KubernetesConfig `yaml:"kubernetes"`
	Metrics    MetricsConfig    `yaml:"metrics"`
}

// MetricsConfig — параметры источника метрик.
type MetricsConfig struct {
	// Source: "auto" (по умолчанию), "metrics-server" или "prometheus".
	// "auto" — источник определяется автоматически: сначала metrics-server
	// (metrics.k8s.io), затем Prometheus (если указан prometheus_url).
	Source string `yaml:"source"`
	// PrometheusURL — адрес Prometheus HTTP API, например "http://prometheus:9090".
	// Используется, если metrics-server недоступен.
	PrometheusURL string `yaml:"prometheus_url"`
}

// ServerConfig — параметры HTTP-сервера.
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// KubernetesConfig — параметры подключения к кластеру.
type KubernetesConfig struct {
	Namespace  string `yaml:"namespace"`
	PodPrefix  string `yaml:"pod_prefix"`
	Kubeconfig string `yaml:"kubeconfig"`
}

// Load читает и валидирует конфигурацию из файла.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Kubernetes.Namespace == "" {
		cfg.Kubernetes.Namespace = "opencode-agents"
	}
	if cfg.Kubernetes.PodPrefix == "" {
		cfg.Kubernetes.PodPrefix = "opencode"
	}
	if cfg.Metrics.Source == "" {
		cfg.Metrics.Source = "auto"
	}
	return cfg, nil
}
