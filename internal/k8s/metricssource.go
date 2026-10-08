package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MetricsSourceKind — тип источника метрик.
type MetricsSourceKind string

const (
	SourceMetricsServer MetricsSourceKind = "metrics-server"
	SourcePrometheus    MetricsSourceKind = "prometheus"
)

// MetricsSource — источник метрик пода.
type MetricsSource interface {
	Kind() MetricsSourceKind
	GetPodMetrics(namespace, pod string) (*PodMetrics, error)
}

// ---------- Metrics Server (metrics.k8s.io) ----------

// MetricsServerSource — метрики из Kubernetes Metrics Server.
type MetricsServerSource struct {
	client *Client
}

func (s *MetricsServerSource) Kind() MetricsSourceKind { return SourceMetricsServer }

func (s *MetricsServerSource) GetPodMetrics(namespace, pod string) (*PodMetrics, error) {
	if s.client.Metrics == nil {
		return nil, fmt.Errorf("metrics client not initialized")
	}
	pm, err := s.client.Metrics.MetricsV1beta1().PodMetricses(namespace).Get(ctx(), pod, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod metrics: %w", err)
	}

	out := &PodMetrics{
		Timestamp:  pm.Timestamp.Time.UTC().Format(time.RFC3339),
		Containers: []ContainerMetrics{},
	}
	var totCPU, totMem, totStorage resource.Quantity
	for _, cm := range pm.Containers {
		cpu := cm.Usage.Cpu()
		mem := cm.Usage.Memory()
		storage := resource.Quantity{}
		if q, ok := cm.Usage[corev1.ResourceEphemeralStorage]; ok {
			storage = q
		}
		out.Containers = append(out.Containers, ContainerMetrics{
			Name:    cm.Name,
			CPU:     cpu.String(),
			Memory:  mem.String(),
			Storage: storage.String(),
		})
		totCPU.Add(*cpu)
		totMem.Add(*mem)
		totStorage.Add(storage)
	}
	out.CPU = totCPU.String()
	out.Memory = totMem.String()
	out.Storage = totStorage.String()
	return out, nil
}

// ---------- Prometheus ----------

// PrometheusSource — метрики из Prometheus HTTP API (PromQL).
type PrometheusSource struct {
	baseURL    string
	httpClient *http.Client
}

func (s *PrometheusSource) Kind() MetricsSourceKind { return SourcePrometheus }

func (s *PrometheusSource) GetPodMetrics(namespace, pod string) (*PodMetrics, error) {
	cpu, err := s.queryVector(s.cpuQuery(namespace, pod))
	if err != nil {
		return nil, fmt.Errorf("prometheus cpu query: %w", err)
	}
	if len(cpu) == 0 {
		return nil, fmt.Errorf("prometheus: no cpu metrics for pod %s/%s", namespace, pod)
	}
	mem, _ := s.queryVector(s.memQuery(namespace, pod))
	sto, _ := s.queryVector(s.stoQuery(namespace, pod))

	names := make([]string, 0, len(cpu))
	for n := range cpu {
		names = append(names, n)
	}
	sort.Strings(names)

	out := &PodMetrics{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Containers: []ContainerMetrics{},
	}
	var totCPU, totMem, totSto resource.Quantity
	for _, name := range names {
		cpuQ := *resource.NewMilliQuantity(int64(cpu[name]*1000), resource.DecimalSI)
		var memQ, stoQ resource.Quantity
		if b, ok := mem[name]; ok {
			memQ = *resource.NewQuantity(int64(b), resource.BinarySI)
		}
		if b, ok := sto[name]; ok {
			stoQ = *resource.NewQuantity(int64(b), resource.BinarySI)
		}
		out.Containers = append(out.Containers, ContainerMetrics{
			Name:    name,
			CPU:     cpuQ.String(),
			Memory:  memQ.String(),
			Storage: stoQ.String(),
		})
		totCPU.Add(cpuQ)
		totMem.Add(memQ)
		totSto.Add(stoQ)
	}
	out.CPU = totCPU.String()
	out.Memory = totMem.String()
	out.Storage = totSto.String()
	return out, nil
}

func (s *PrometheusSource) cpuQuery(ns, pod string) string {
	return fmt.Sprintf(`sum by (container) (rate(container_cpu_usage_seconds_total{namespace="%s", pod="%s", container!="", container!="POD"}[5m]))`, ns, pod)
}
func (s *PrometheusSource) memQuery(ns, pod string) string {
	return fmt.Sprintf(`sum by (container) (container_memory_working_set_bytes{namespace="%s", pod="%s", container!="", container!="POD"})`, ns, pod)
}
func (s *PrometheusSource) stoQuery(ns, pod string) string {
	return fmt.Sprintf(`sum by (container) (container_fs_usage_bytes{namespace="%s", pod="%s", container!="", container!="POD"})`, ns, pod)
}

// queryVector выполняет мгновенный запрос к Prometheus и возвращает
// агрегированные значения по контейнерам (container -> значение).
func (s *PrometheusSource) queryVector(promql string) (map[string]float64, error) {
	req, err := http.NewRequest(http.MethodGet, s.baseURL+"/api/v1/query", nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = url.Values{"query": []string{promql}}.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("prometheus returned %d: %s", resp.StatusCode, string(body))
	}

	var out struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []interface{}     `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	res := map[string]float64{}
	for _, r := range out.Data.Result {
		if len(r.Value) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(fmt.Sprint(r.Value[1]), 64)
		name := r.Metric["container"]
		if name == "" {
			name = "unknown"
		}
		res[name] += v
	}
	return res, nil
}

// ---------- Определение источника (как в Lens) ----------

// resolveSources определяет доступные источники метрик (Prometheus или
// Kubernetes Metrics Server) и кэширует результат.
func (c *Client) resolveSources() []MetricsSource {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sourcesResolved {
		return c.sources
	}
	c.sources, c.sourceErr = c.detectSources()
	c.sourcesResolved = true
	return c.sources
}

// RedetectSources сбрасывает кэш и заставляет повторно определить источники.
func (c *Client) RedetectSources() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sourcesResolved = false
	c.sources = nil
	c.sourceErr = nil
}

// detectSources пробует каждый кандидат и возвращает упорядоченный список
// доступных источников. Приоритет зависит от настройки metrics.source.
func (c *Client) detectSources() ([]MetricsSource, error) {
	msAvail := c.metricsServerAvailable()
	prAvail := c.prometheusAvailable()

	ms := func() MetricsSource { return &MetricsServerSource{client: c} }
	pr := func() MetricsSource {
		return &PrometheusSource{
			baseURL:    c.prometheusURL,
			httpClient: &http.Client{Timeout: 15 * time.Second},
		}
	}

	var sources []MetricsSource
	switch c.metricsSource {
	case string(SourcePrometheus):
		if prAvail {
			sources = append(sources, pr())
		}
		if msAvail {
			sources = append(sources, ms())
		}
	case string(SourceMetricsServer):
		if msAvail {
			sources = append(sources, ms())
		}
		if prAvail {
			sources = append(sources, pr())
		}
	default: // auto
		if msAvail {
			sources = append(sources, ms())
		}
		if prAvail {
			sources = append(sources, pr())
		}
	}

	if len(sources) > 0 {
		return sources, nil
	}
	return nil, fmt.Errorf("metrics source not found: metrics-server (metrics.k8s.io) is not served by the cluster and prometheus is not configured or unreachable (set metrics.prometheus_url)")
}

// metricsServerAvailable проверяет, обслуживает ли кластер API metrics.k8s.io.
func (c *Client) metricsServerAvailable() bool {
	if c.Metrics == nil || c.Discovery == nil {
		return false
	}
	groups, err := c.Discovery.ServerGroups()
	if err != nil {
		return false
	}
	for _, g := range groups.Groups {
		if g.Name == "metrics.k8s.io" {
			return true
		}
	}
	return false
}

// prometheusAvailable проверяет доступность Prometheus HTTP API.
func (c *Client) prometheusAvailable() bool {
	if c.prometheusURL == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, c.prometheusURL+"/api/v1/query", nil)
	if err != nil {
		return false
	}
	req.URL.RawQuery = url.Values{"query": []string{"up"}}.Encode()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// MetricsSourceInfo — информация об источнике метрик для API.
type MetricsSourceInfo struct {
	Active    string   `json:"active"`
	Available []string `json:"available"`
	Error     string   `json:"error,omitempty"`
}

// GetMetricsSourceInfo возвращает активный и доступные источники метрик.
func (c *Client) GetMetricsSourceInfo() MetricsSourceInfo {
	sources := c.resolveSources()
	info := MetricsSourceInfo{Available: []string{}}
	for _, s := range sources {
		info.Available = append(info.Available, string(s.Kind()))
	}
	if len(sources) > 0 {
		info.Active = string(sources[0].Kind())
	}
	if c.sourceErr != nil {
		info.Error = c.sourceErr.Error()
	}
	return info
}