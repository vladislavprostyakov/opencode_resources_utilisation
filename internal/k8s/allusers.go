package k8s

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetAllUsersInfo собирает сводные данные по всем пользователям namespace.
// Свободные ресурсы считаются от установленной квоты namespace, если она задана,
// иначе — от общего числа доступных в кластере ресурсов.
func (c *Client) GetAllUsersInfo() (*AllUsersInfo, error) {
	pods, err := c.Kube.CoreV1().Pods(c.Namespace).List(ctx(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	type acc struct {
		cpu, mem, eph resource.Quantity
		cpuLim, memLim, ephLim resource.Quantity
		count         int
	}
	byUser := map[string]*acc{}
	totalPods := 0

	var totCPU, totMem, totEph, totCPULim, totMemLim, totEphLim resource.Quantity

	for _, p := range pods.Items {
		user, ok := c.parseUser(p.Name)
		if !ok {
			continue
		}
		totalPods++
		a := byUser[user]
		if a == nil {
			a = &acc{}
			byUser[user] = a
		}
		a.count++
		for _, cs := range p.Spec.Containers {
			if q, ok := cs.Resources.Requests[corev1.ResourceCPU]; ok {
				a.cpu.Add(q)
				totCPU.Add(q)
			}
			if q, ok := cs.Resources.Limits[corev1.ResourceCPU]; ok {
				a.cpuLim.Add(q)
				totCPULim.Add(q)
			}
			if q, ok := cs.Resources.Requests[corev1.ResourceMemory]; ok {
				a.mem.Add(q)
				totMem.Add(q)
			}
			if q, ok := cs.Resources.Limits[corev1.ResourceMemory]; ok {
				a.memLim.Add(q)
				totMemLim.Add(q)
			}
			if q, ok := cs.Resources.Requests[corev1.ResourceEphemeralStorage]; ok {
				a.eph.Add(q)
				totEph.Add(q)
			}
			if q, ok := cs.Resources.Limits[corev1.ResourceEphemeralStorage]; ok {
				a.ephLim.Add(q)
				totEphLim.Add(q)
			}
		}
	}

	users := make([]UserSummary, 0, len(byUser))
	for u, a := range byUser {
		users = append(users, UserSummary{
			User:             u,
			PodCount:         a.count,
			CPURequest:       cpuStr(a.cpu),
			CPULimit:         cpuStr(a.cpuLim),
			MemoryRequest:    memStr(a.mem),
			MemoryLimit:      memStr(a.memLim),
			EphemeralRequest: memStr(a.eph),
			EphemeralLimit:   memStr(a.ephLim),
		})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].User < users[j].User })

	quota, hard, used := c.getNamespaceQuota()
	cluster, _ := c.GetClusterInfo()

	info := &AllUsersInfo{
		Namespace: c.Namespace,
		Cluster:   cluster,
		Quota:     quota,
		Users:     users,
		Totals: PodTotals{
			PodCount:         totalPods,
			MemoryRequest:    memStr(totMem),
			MemoryLimit:      memStr(totMemLim),
			CPURequest:       cpuStr(totCPU),
			CPULimit:         cpuStr(totCPULim),
			EphemeralRequest: memStr(totEph),
			EphemeralLimit:   memStr(totEphLim),
		},
	}
	info.Free = computeFreeResources(hard, used, cluster)
	return info, nil
}

// computeFreeResources определяет свободные ресурсы namespace.
// Если для ресурса задан лимит квоты — свободные считаются как лимит минус
// использованное; иначе — как доступные ресурсы кластера (для ephemeral — пусто).
func computeFreeResources(hard, used map[corev1.ResourceName]resource.Quantity, cluster *ClusterInfo) FreeResources {
	clusterCPU, clusterMem := "", ""
	if cluster != nil {
		clusterCPU = cluster.AvailableCPU
		clusterMem = cluster.AvailableMemory
	}
	source := "cluster"
	if len(hard) > 0 {
		source = "quota"
	}
	return FreeResources{
		Source:    source,
		CPU:       freeResource(hard, used, corev1.ResourceCPU, clusterCPU),
		Memory:    freeResource(hard, used, corev1.ResourceMemory, clusterMem),
		Ephemeral: freeResource(hard, used, corev1.ResourceEphemeralStorage, ""),
	}
}

// freeResource возвращает свободный ресурс: от квоты, если лимит задан,
// иначе — переданное значение из доступных ресурсов кластера.
func freeResource(hard, used map[corev1.ResourceName]resource.Quantity, name corev1.ResourceName, clusterFallback string) string {
	if h, ok := hard[name]; ok && !h.IsZero() {
		rem := h.DeepCopy()
		rem.Sub(used[name])
		return quotaValStr(name, rem)
	}
	return clusterFallback
}
