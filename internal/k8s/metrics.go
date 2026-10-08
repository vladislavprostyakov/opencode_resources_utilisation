package k8s

import (
	"fmt"
)

// GetPodMetrics возвращает текущие метрики пода, используя доступный источник
// метрик (Kubernetes Metrics Server или Prometheus). Возвращает (nil, err),
// если ни один источник не смог предоставить данные.
func (c *Client) GetPodMetrics(pod string) (*PodMetrics, error) {
	sources := c.resolveSources()
	if len(sources) == 0 {
		msg := "no metrics source available"
		if c.sourceErr != nil {
			msg = c.sourceErr.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	var lastErr error
	for _, s := range sources {
		m, err := s.GetPodMetrics(c.Namespace, pod)
		if err == nil && m != nil {
			return m, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("all metrics sources failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no metrics returned for pod %s", pod)
}

