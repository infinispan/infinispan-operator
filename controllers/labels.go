package controllers

// LabelsResource returns the labels that must me applied to the resource
func LabelsResource(name, resourceType string) map[string]string {
	m := map[string]string{"infinispan_cr": name, "clusterName": name}
	if resourceType != "" {
		m["app"] = resourceType
	}
	return m
}

func PodLabels(name string) map[string]string {
	return LabelsResource(name, "infinispan-pod")
}

func ServiceLabels(name string) map[string]string {
	return map[string]string{
		"clusterName": name,
		"app":         "infinispan-pod",
	}
}

func ExternalServiceLabels(name string) map[string]string {
	return LabelsResource(name, "infinispan-service-external")
}

func BackupPodLabels(backup, cluster string) map[string]string {
	m := ServiceLabels(cluster)
	m["backup_cr"] = backup
	return m
}

func RestorePodLabels(backup, cluster string) map[string]string {
	m := ServiceLabels(cluster)
	m["restore_cr"] = backup
	return m
}

func BatchLabels(name string) map[string]string {
	return map[string]string{
		"infinispan_batch": name,
		"app":              "infinispan-batch-pod",
	}
}
