package k8s

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func memStr(q resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}
	return q.String()
}

func cpuStr(q resource.Quantity) string {
	if q.IsZero() {
		return "0 cores"
	}
	return strconv.FormatFloat(q.AsApproximateFloat64(), 'f', -1, 64) + " cores"
}

func totalRestarts(status *corev1.PodStatus) int32 {
	var total int32
	for _, cs := range status.ContainerStatuses {
		total += cs.RestartCount
	}
	for _, cs := range status.InitContainerStatuses {
		total += cs.RestartCount
	}
	return total
}

func containerSpec(p *corev1.Pod, name string) string {
	for _, cs := range p.Spec.Containers {
		if cs.Name == name {
			return cs.Image
		}
	}
	return ""
}

func describeState(s *corev1.ContainerState) string {
	switch {
	case s.Running != nil:
		if s.Running.StartedAt.IsZero() {
			return "running"
		}
		return "running (с " + s.Running.StartedAt.UTC().Format("2006-01-02 15:04:05") + ")"
	case s.Waiting != nil:
		return "waiting: " + s.Waiting.Reason
	case s.Terminated != nil:
		return "terminated: " + s.Terminated.Reason
	default:
		return "unknown"
	}
}

// friendlyReason переводит причину перезапуска в понятную формулировку.
func friendlyReason(reason, message string) string {
	switch reason {
	case "OOMKilled":
		return "нехватка памяти (OOM): контейнер остановлен, исчерпал выделенную память"
	case "Error":
		return "ошибка выполнения: процесс завершился с ненулевым кодом"
	case "Evicted":
		return "удалён из-за нехватки ресурсов на ноде (eviction)"
	case "ContainerCannotRun":
		return "не удалось запустить контейнер"
	case "CrashLoopBackOff":
		return "повторные сбои запуска (CrashLoopBackOff)"
	case "Completed":
		return "завершил работу успешно"
	case "":
		if message != "" {
			return message
		}
		return "причина не указана"
	default:
		if message != "" {
			return reason + ": " + message
		}
		return reason
	}
}
