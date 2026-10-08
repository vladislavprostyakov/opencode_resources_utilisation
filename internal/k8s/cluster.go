package k8s

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetClusterInfo — суммарные и доступные ресурсы кластера.
func (c *Client) GetClusterInfo() (*ClusterInfo, error) {
	nodes, err := c.Kube.CoreV1().Nodes().List(ctx(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	var totalCPU, totalMem resource.Quantity
	for _, n := range nodes.Items {
		totalCPU.Add(*n.Status.Allocatable.Cpu())
		totalMem.Add(*n.Status.Allocatable.Memory())
	}

	pods, err := c.Kube.CoreV1().Pods("").List(ctx(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list cluster pods: %w", err)
	}
	var reqCPU, reqMem resource.Quantity
	for _, p := range pods.Items {
		if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending {
			continue
		}
		for _, cs := range p.Spec.Containers {
			if q, ok := cs.Resources.Requests[corev1.ResourceCPU]; ok {
				reqCPU.Add(q)
			}
			if q, ok := cs.Resources.Requests[corev1.ResourceMemory]; ok {
				reqMem.Add(q)
			}
		}
	}

	availCPU := totalCPU.DeepCopy()
	availCPU.Sub(reqCPU)
	availMem := totalMem.DeepCopy()
	availMem.Sub(reqMem)

	return &ClusterInfo{
		NodeCount:       len(nodes.Items),
		TotalCPU:        cpuStr(totalCPU),
		TotalMemory:     memStr(totalMem),
		RequestedCPU:    cpuStr(reqCPU),
		RequestedMemory: memStr(reqMem),
		AvailableCPU:    cpuStr(availCPU),
		AvailableMemory: memStr(availMem),
	}, nil
}
