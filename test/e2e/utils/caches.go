package utils

import (
	"strconv"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// Populate the cache using a Job inside the test namespace so that large number of posts
// don't have to be executed from outside the k8s cluster
func (k *TestKubernetes) PopulateCache(cacheName, host, value string, contentType mime.MimeType, numEntries int, i *v1.Infinispan) {
	jobName := i.Name
	namespace := i.Namespace
	command := `#!/bin/sh
    if [[ -n $USER ]]; then
      AUTH="--digest -u $USER:$PASS"
    fi
    for (( i=0; i<$NUM_ENTRIES; i++ ))
    do
      curl $AUTH -X POST --data-binary "$VALUE" -H "Content-Type: $CONTENT_TYPE"  $HOST/rest/v2/caches/$CACHE/$i
    done
	`
	user, pass := UserAndPassword(i, k)
	labels := map[string]string{"app": "populate-cache"}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: pointer.Int32Ptr(0),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "populate-cache",
						Image:   i.ImageName(),
						Command: []string{"sh", "-c"},
						Args:    []string{command},
						Env: []corev1.EnvVar{
							{Name: "USER", Value: user},
							{Name: "PASS", Value: pass},
							{Name: "NUM_ENTRIES", Value: strconv.Itoa(numEntries)},
							{Name: "HOST", Value: host},
							{Name: "CACHE", Value: cacheName},
							{Name: "VALUE", Value: value},
							{Name: "CONTENT_TYPE", Value: string(contentType)},
						},
					}},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
		},
	}
	k.Create(job)
	defer k.DeleteJob(job)
	k.WaitForJobToComplete(jobName, namespace, labels)
}
