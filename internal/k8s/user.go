package k8s

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetUserInfo собирает всю информацию по пользователю.
func (c *Client) GetUserInfo(user string) (*UserInfo, error) {
	pods, err := c.Kube.CoreV1().Pods(c.Namespace).List(ctx(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	events, _ := c.Kube.CoreV1().Events(c.Namespace).List(ctx(), metav1.ListOptions{})

	info := &UserInfo{
		User:                user,
		Namespace:           c.Namespace,
		Pods:                []PodInfo{},
		QuotaUtilizationPct: map[string]float64{},
		PVCs:                []PVCInfo{},
	}

	var userCPU, userMem, userEph resource.Quantity
	pvcNames := map[string]struct{}{}

	for _, p := range pods.Items {
		pu, ok := c.parseUser(p.Name)
		if !ok || pu != user {
			continue
		}
		podInfo := c.buildPodInfo(&p, events.Items)
		info.Pods = append(info.Pods, podInfo.PodInfo)

		userCPU.Add(podInfo.cpuRequest)
		userMem.Add(podInfo.memRequest)
		userEph.Add(podInfo.ephRequest)

		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				pvcNames[v.PersistentVolumeClaim.ClaimName] = struct{}{}
			}
		}
	}

	sort.Slice(info.Pods, func(i, j int) bool { return info.Pods[i].Name < info.Pods[j].Name })

	info.Totals = PodTotals{
		PodCount:         len(info.Pods),
		MemoryRequest:    memStr(userMem),
		MemoryLimit:      memStr(sumLimit(info.Pods, memLimitKey)),
		CPURequest:       cpuStr(userCPU),
		CPULimit:         cpuStr(sumLimit(info.Pods, cpuLimitKey)),
		EphemeralRequest: memStr(userEph),
		EphemeralLimit:   memStr(sumLimit(info.Pods, ephLimitKey)),
	}

	quota, hard, _ := c.getNamespaceQuota()
	info.Quota = quota
	info.QuotaUtilizationPct = userQuotaUtilization(hard, userCPU, userMem, userEph, len(info.Pods))

	info.PVCs = c.getUserPVCs(pvcNames, userEph)

	if cluster, err := c.GetClusterInfo(); err == nil {
		info.Cluster = cluster
	}

	return info, nil
}
