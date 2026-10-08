package k8s

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// parseUser извлекает имя пользователя из имени пода.
// Формат: <prefix>-<user>-<mail-suffix>-<replicaset-hash>-<pod-hash>
// Имя пользователя — второе поле по разделителю "-".
func (c *Client) parseUser(podName string) (string, bool) {
	parts := strings.Split(podName, "-")
	if len(parts) < 2 || parts[0] != c.PodPrefix {
		return "", false
	}
	user := parts[1]
	if user == "" {
		return "", false
	}
	return user, true
}

// ListUsers возвращает отсортированный список уникальных пользователей,
// определённых по именам подов в namespace.
func (c *Client) ListUsers() ([]string, error) {
	pods, err := c.Kube.CoreV1().Pods(c.Namespace).List(ctx(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	set := map[string]struct{}{}
	for _, p := range pods.Items {
		if user, ok := c.parseUser(p.Name); ok {
			set[user] = struct{}{}
		}
	}
	users := make([]string, 0, len(set))
	for u := range set {
		users = append(users, u)
	}
	sort.Strings(users)
	return users, nil
}

// GetPodLogs возвращает логи пода. Если allContainers=true, логи собираются
// по всем контейнерам пода (с заголовками), иначе — по единственному/основному.
func (c *Client) GetPodLogs(pod string, tail int64, allContainers bool) (string, error) {
	p, err := c.Kube.CoreV1().Pods(c.Namespace).Get(ctx(), pod, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("get pod: %w", err)
	}

	var containers []string
	if allContainers {
		for _, cs := range p.Spec.Containers {
			containers = append(containers, cs.Name)
		}
	}
	if len(containers) == 0 {
		containers = []string{""}
	}

	var sb strings.Builder
	for _, name := range containers {
		opts := &corev1.PodLogOptions{TailLines: &tail}
		if name != "" {
			opts.Container = name
		}
		req := c.Kube.CoreV1().Pods(c.Namespace).GetLogs(pod, opts)
		stream, err := req.Stream(ctx())
		if err != nil {
			if len(containers) > 1 {
				sb.WriteString(fmt.Sprintf("[контейнер %s] ошибка: %v\n", name, err))
				continue
			}
			return "", fmt.Errorf("get logs: %w", err)
		}
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, stream); err != nil {
			stream.Close()
			return "", fmt.Errorf("read logs: %w", err)
		}
		stream.Close()
		if len(containers) > 1 {
			sb.WriteString(fmt.Sprintf("===== контейнер: %s =====\n", name))
		}
		sb.WriteString(buf.String())
		if !strings.HasSuffix(buf.String(), "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

// buildPodInfo формирует PodInfo по одному поду.
func (c *Client) buildPodInfo(p *corev1.Pod, events []corev1.Event) podInfoWithQuantities {
	pi := podInfoWithQuantities{PodInfo: PodInfo{
		Name:      p.Name,
		Namespace: p.Namespace,
		Status:    string(p.Status.Phase),
		Node:      p.Spec.NodeName,
		CreatedAt: p.CreationTimestamp.UTC().Format(time.RFC3339),
		Uptime:    time.Since(p.CreationTimestamp.Time).Round(time.Second).String(),
		Events:    []EventInfo{},
	}}

	var cpuReq, cpuLim, memReq, memLim, ephReq, ephLim resource.Quantity
	for _, cs := range p.Spec.Containers {
		if q, ok := cs.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuReq.Add(q)
		}
		if q, ok := cs.Resources.Limits[corev1.ResourceCPU]; ok {
			cpuLim.Add(q)
		}
		if q, ok := cs.Resources.Requests[corev1.ResourceMemory]; ok {
			memReq.Add(q)
		}
		if q, ok := cs.Resources.Limits[corev1.ResourceMemory]; ok {
			memLim.Add(q)
		}
		if q, ok := cs.Resources.Requests[corev1.ResourceEphemeralStorage]; ok {
			ephReq.Add(q)
		}
		if q, ok := cs.Resources.Limits[corev1.ResourceEphemeralStorage]; ok {
			ephLim.Add(q)
		}
	}
	pi.cpuRequest = cpuReq
	pi.memRequest = memReq
	pi.ephRequest = ephReq
	pi.PodInfo.CPURequest = cpuStr(cpuReq)
	pi.PodInfo.CPULimit = cpuStr(cpuLim)
	pi.PodInfo.MemoryRequest = memStr(memReq)
	pi.PodInfo.MemoryLimit = memStr(memLim)
	pi.PodInfo.EphemeralRequest = memStr(ephReq)
	pi.PodInfo.EphemeralLimit = memStr(ephLim)

	reasons := []string{}
	for i := range p.Status.ContainerStatuses {
		cs := &p.Status.ContainerStatuses[i]
		ci := ContainerInfo{
			Name:         cs.Name,
			Image:        containerSpec(p, cs.Name),
			Ready:        cs.Ready,
			RestartCount: cs.RestartCount,
			State:        describeState(&cs.State),
			LastState:    describeState(&cs.LastTerminationState),
		}
		if cs.LastTerminationState.Terminated != nil {
			ci.LastStateReason = friendlyReason(cs.LastTerminationState.Terminated.Reason, cs.LastTerminationState.Terminated.Message)
		}
		pi.PodInfo.Containers = append(pi.PodInfo.Containers, ci)
		if cs.RestartCount > 0 {
			reason := ci.LastStateReason
			if reason == "" {
				reason = "перезапуск(и)"
			}
			reasons = append(reasons, fmt.Sprintf("контейнер %q: %d раз(а), %s", cs.Name, cs.RestartCount, reason))
		}
	}
	pi.PodInfo.RestartCount = totalRestarts(&p.Status)
	pi.PodInfo.RestartReasons = reasons

	for _, e := range events {
		if e.InvolvedObject.Name == p.Name && e.InvolvedObject.Kind == "Pod" {
			ts := ""
			if !e.LastTimestamp.IsZero() {
				ts = e.LastTimestamp.UTC().Format(time.RFC3339)
			}
			pi.PodInfo.Events = append(pi.PodInfo.Events, EventInfo{
				Type:          e.Type,
				Reason:        e.Reason,
				Message:       e.Message,
				Count:         e.Count,
				LastTimestamp: ts,
			})
		}
	}
	sort.Slice(pi.PodInfo.Events, func(i, j int) bool {
		return pi.PodInfo.Events[i].LastTimestamp > pi.PodInfo.Events[j].LastTimestamp
	})

	if m, _ := c.GetPodMetrics(p.Name); m != nil {
		pi.PodInfo.Metrics = m
	}

	return pi
}

// podInfoWithQuantities — внутренняя структура с сырыми количествами.
type podInfoWithQuantities struct {
	PodInfo
	cpuRequest resource.Quantity
	memRequest resource.Quantity
	ephRequest resource.Quantity
}

type limitKey int

const (
	memLimitKey limitKey = iota
	cpuLimitKey
	ephLimitKey
)

func sumLimit(pods []PodInfo, key limitKey) resource.Quantity {
	var total resource.Quantity
	for _, p := range pods {
		switch key {
		case memLimitKey:
			total.Add(parseQty(p.MemoryLimit, false))
		case cpuLimitKey:
			total.Add(parseQty(p.CPULimit, true))
		case ephLimitKey:
			total.Add(parseQty(p.EphemeralLimit, false))
		}
	}
	return total
}

func parseQty(s string, isCPU bool) resource.Quantity {
	s = strings.TrimSuffix(s, " cores")
	q, err := resource.ParseQuantity(strings.TrimSpace(s))
	if err != nil {
		return resource.Quantity{}
	}
	return q
}
