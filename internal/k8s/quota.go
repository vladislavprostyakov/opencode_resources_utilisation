package k8s

import (
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// getNamespaceQuota агрегирует квоты namespace.
// Возвращает агрегированную квоту, а также карты установленных лимитов (hard)
// и фактически использованных ресурсов (used).
func (c *Client) getNamespaceQuota() (*NamespaceQuota, map[corev1.ResourceName]resource.Quantity, map[corev1.ResourceName]resource.Quantity) {
	quotas, err := c.Kube.CoreV1().ResourceQuotas(c.Namespace).List(ctx(), metav1.ListOptions{})
	if err != nil {
		return nil, nil, nil
	}
	hard := map[corev1.ResourceName]resource.Quantity{}
	used := map[corev1.ResourceName]resource.Quantity{}
	for _, q := range quotas.Items {
		for k, v := range q.Spec.Hard {
			hard[k] = addQty(hard[k], v)
		}
		for k, v := range q.Status.Used {
			used[k] = addQty(used[k], v)
		}
	}

	nq := &NamespaceQuota{Items: []QuotaItem{}}
	names := make([]corev1.ResourceName, 0, len(hard))
	for k := range hard {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return string(names[i]) < string(names[j]) })
	for _, k := range names {
		h := hard[k]
		u := used[k]
		rem := h.DeepCopy()
		rem.Sub(u)
		pct := 0.0
		if !h.IsZero() {
			pct = float64(u.MilliValue()) / float64(h.MilliValue()) * 100
		}
		nq.Items = append(nq.Items, QuotaItem{
			Resource:    string(k),
			Limit:       quotaValStr(k, h),
			Used:        quotaValStr(k, u),
			Remaining:   quotaValStr(k, rem),
			Utilization: pct,
		})
	}
	return nq, hard, used
}

// userQuotaUtilization — процент использования квоты подами конкретного пользователя.
func userQuotaUtilization(hard map[corev1.ResourceName]resource.Quantity,
	cpu, mem, eph resource.Quantity, podCount int) map[string]float64 {

	res := map[string]float64{}
	if len(hard) == 0 {
		return res
	}
	usedBy := map[corev1.ResourceName]resource.Quantity{
		corev1.ResourceCPU:              cpu,
		corev1.ResourceMemory:           mem,
		corev1.ResourceEphemeralStorage: eph,
		corev1.ResourcePods:             *resource.NewQuantity(int64(podCount), resource.DecimalSI),
	}
	for k, h := range hard {
		if h.IsZero() {
			continue
		}
		u, ok := usedBy[k]
		if !ok {
			continue
		}
		res[string(k)] = float64(u.MilliValue()) / float64(h.MilliValue()) * 100
	}
	return res
}

// getUserPVCs собирает PVC, на которые ссылаются поды пользователя.
func (c *Client) getUserPVCs(names map[string]struct{}, podEphUsage resource.Quantity) []PVCInfo {
	out := []PVCInfo{}
	for name := range names {
		pvc, err := c.Kube.CoreV1().PersistentVolumeClaims(c.Namespace).Get(ctx(), name, metav1.GetOptions{})
		if err != nil {
			continue
		}
		capacity := ""
		if cap, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			capacity = cap.String()
		}
		am := make([]string, 0, len(pvc.Spec.AccessModes))
		for _, m := range pvc.Spec.AccessModes {
			am = append(am, string(m))
		}
		sc := ""
		if pvc.Spec.StorageClassName != nil {
			sc = *pvc.Spec.StorageClassName
		}
		info := PVCInfo{
			Name:         pvc.Name,
			Status:       string(pvc.Status.Phase),
			Capacity:     capacity,
			AccessModes:  am,
			StorageClass: sc,
		}
		// Приближённая утилизация: ephemeral storage usage пода / capacity PVC.
		if cap, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok && !cap.IsZero() && !podEphUsage.IsZero() {
			usageStr := podEphUsage.String()
			pct := float64(podEphUsage.Value()) / float64(cap.Value()) * 100
			info.Usage = &usageStr
			info.Utilization = &pct
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func addQty(a, b resource.Quantity) resource.Quantity {
	if a.IsZero() {
		return b.DeepCopy()
	}
	a.Add(b)
	return a
}

func quotaValStr(k corev1.ResourceName, q resource.Quantity) string {
	switch k {
	case corev1.ResourceCPU:
		return cpuStr(q)
	case corev1.ResourcePods:
		return strconv.FormatInt(q.Value(), 10)
	default:
		return memStr(q)
	}
}
