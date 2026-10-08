package k8s

// ClusterInfo — сводка по ресурсам всего кластера.
type ClusterInfo struct {
	NodeCount       int    `json:"node_count"`
	TotalCPU        string `json:"total_cpu"`
	TotalMemory     string `json:"total_memory"`
	RequestedCPU    string `json:"requested_cpu"`
	RequestedMemory string `json:"requested_memory"`
	AvailableCPU    string `json:"available_cpu"`
	AvailableMemory string `json:"available_memory"`
}

// QuotaItem — одна позиция квоты namespace.
type QuotaItem struct {
	Resource    string  `json:"resource"`
	Limit       string  `json:"limit"`
	Used        string  `json:"used"`
	Remaining   string  `json:"remaining"`
	Utilization float64 `json:"utilization_pct"`
}

// NamespaceQuota — агрегированная квота namespace.
type NamespaceQuota struct {
	Items []QuotaItem `json:"items"`
}

// ContainerInfo — состояние контейнера.
type ContainerInfo struct {
	Name            string `json:"name"`
	Image           string `json:"image"`
	Ready           bool   `json:"ready"`
	RestartCount    int32  `json:"restart_count"`
	State           string `json:"state"`
	LastState       string `json:"last_state"`
	LastStateReason string `json:"last_state_reason"`
}

// EventInfo — событие кластера.
type EventInfo struct {
	Type          string `json:"type"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Count         int32  `json:"count"`
	LastTimestamp string `json:"last_timestamp"`
}

// ContainerMetrics — метрики одного контейнера.
type ContainerMetrics struct {
	Name    string `json:"name"`
	CPU     string `json:"cpu"`
	Memory  string `json:"memory"`
	Storage string `json:"ephemeral_storage"`
}

// PodMetrics — текущие метрики пода (из metrics-server).
type PodMetrics struct {
	Timestamp  string             `json:"timestamp"`
	CPU        string             `json:"cpu"`
	Memory     string             `json:"memory"`
	Storage    string             `json:"ephemeral_storage"`
	Containers []ContainerMetrics `json:"containers"`
}

// PodInfo — полная информация о поде.
type PodInfo struct {
	Name             string          `json:"name"`
	Namespace        string          `json:"namespace"`
	Status           string          `json:"status"`
	Node             string          `json:"node"`
	CreatedAt        string          `json:"created_at"`
	Uptime           string          `json:"uptime"`
	RestartCount     int32           `json:"restart_count"`
	RestartReasons   []string        `json:"restart_reasons"`
	Containers       []ContainerInfo `json:"containers"`
	MemoryRequest    string          `json:"memory_request"`
	MemoryLimit      string          `json:"memory_limit"`
	CPURequest       string          `json:"cpu_request"`
	CPULimit         string          `json:"cpu_limit"`
	EphemeralRequest string          `json:"ephemeral_storage_request"`
	EphemeralLimit   string          `json:"ephemeral_storage_limit"`
	Events           []EventInfo     `json:"events"`
	Metrics          *PodMetrics     `json:"metrics"`
}

// PVCInfo — информация о persistent volume claim.
type PVCInfo struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Capacity     string   `json:"capacity"`
	AccessModes  []string `json:"access_modes"`
	StorageClass string   `json:"storage_class"`
	Usage        *string  `json:"usage"`
	Utilization  *float64 `json:"utilization_pct"`
}

// PodTotals — суммарные request/limit по подам пользователя.
type PodTotals struct {
	PodCount         int    `json:"pod_count"`
	MemoryRequest    string `json:"memory_request"`
	MemoryLimit      string `json:"memory_limit"`
	CPURequest       string `json:"cpu_request"`
	CPULimit         string `json:"cpu_limit"`
	EphemeralRequest string `json:"ephemeral_storage_request"`
	EphemeralLimit   string `json:"ephemeral_storage_limit"`
}

// UserInfo — полная информация по пользователю.
type UserInfo struct {
	User                string           `json:"user"`
	Namespace           string           `json:"namespace"`
	Pods                []PodInfo        `json:"pods"`
	Totals              PodTotals        `json:"totals"`
	Quota               *NamespaceQuota  `json:"quota"`
	QuotaUtilizationPct map[string]float64 `json:"quota_utilization_pct"`
	PVCs                []PVCInfo        `json:"pvcs"`
	Cluster             *ClusterInfo     `json:"cluster"`
}
