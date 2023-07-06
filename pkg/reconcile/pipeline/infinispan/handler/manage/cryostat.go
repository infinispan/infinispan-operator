package manage

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	cryostatv1beta1 "github.com/infinispan/infinispan-operator/pkg/apis/cryostat/v1beta1"
	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/http/curl"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	CryostatPort           = 8181
	CryostatCredentialsApi = "api/v2.2/credentials"
	requeueDelay           = time.Second
)

// CryostatCredentials initialises Cryostat credential store
// RequeueEventually is used throughout this function as failed Cryostat operations should not cause subsequent executions
// in the pipeline to fail
func CryostatCredentials(i *v1.Infinispan, ctx pipeline.Context) {
	if !ctx.IsTypeSupported(pipeline.CryostatGVK) {
		return
	}

	log := ctx.Log()
	cryostat := &cryostatv1beta1.Cryostat{}
	if err := ctx.Resources().Load(i.GetCryostatName(), cryostat); err != nil {
		log.Error(err, "unable to load Cryostat CR")
		ctx.RequeueEventually(requeueDelay)
		return
	}

	// Ensure that the Cryostat deployment is ready before proceeding
	availableCondition := metav1.Condition{Type: string(cryostatv1beta1.ConditionTypeMainDeploymentAvailable)}
	for _, condition := range cryostat.Status.Conditions {
		if condition.Type == string(cryostatv1beta1.ConditionTypeMainDeploymentAvailable) {
			availableCondition = condition
			break
		}
	}
	if availableCondition.Status != metav1.ConditionTrue {
		log.Info("Waiting for Cryostat deployment", "status", availableCondition.Status, "reason", availableCondition.Reason)
		ctx.RequeueEventually(requeueDelay)
		return
	}

	cryostatLabels := map[string]string{
		"app":  cryostat.Name,
		"kind": "cryostat",
	}
	podList := &corev1.PodList{}
	if err := ctx.Resources().List(cryostatLabels, podList); err != nil || len(podList.Items) == 0 {
		log.Error(err, "unable to retrieve Cryostat pod")
		ctx.RequeueEventually(requeueDelay)
		return
	}

	var cryostatPod string
	for _, pod := range podList.Items {
		if kube.IsPodReady(pod) {
			cryostatPod = pod.Name
			break
		}
	}

	if cryostatPod == "" {
		log.Info("Waiting for Cryostat Pod to become Ready")
		ctx.RequeueEventually(requeueDelay)
		return
	}

	client := curl.New(curl.Config{
		Container: cryostat.Name,
		Namespace: cryostat.Namespace,
		Podname:   cryostatPod,
		Protocol:  "https",
		Port:      8181,
	}, ctx.Kubernetes())

	token := base64.StdEncoding.EncodeToString([]byte(ctx.Kubernetes().RestConfig.BearerToken))
	headers := map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", token),
	}

	// If the credential already exists, do nothing
	if exists, err := credentialExists(headers, client, i); err != nil {
		log.Error(err, "unable to determine if Cryostat credential already exists")
		ctx.RequeueEventually(requeueDelay)
	} else if !exists {
		if err := createCredential(headers, client, i, ctx); err != nil {
			log.Error(err, "unable to add Cryostat credentials")
			ctx.RequeueEventually(requeueDelay)
		}
	}
}

func credentialExists(headers map[string]string, client httpClient.HttpClient, i *v1.Infinispan) (bool, error) {
	rsp, err := client.Get(CryostatCredentialsApi, headers)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err := httpClient.ValidateResponse(rsp, err, "checking Cryostat credential", http.StatusOK); err != nil {
		return false, err
	}

	type payload struct {
		Data struct {
			Result []struct {
				MatchExpression string `json:"matchExpression"`
			} `json:"result"`
		} `json:"data"`
	}

	rspBody, err := io.ReadAll(rsp.Body)
	if err != nil {
		return false, fmt.Errorf("unable to read response body: %w", err)
	}

	parsedRsp := &payload{}
	if err := json.Unmarshal(rspBody, parsedRsp); err != nil {
		return false, fmt.Errorf("unable to parse JSON: %w", err)
	}

	matchExpression := MatchExpression(i)
	for _, cred := range parsedRsp.Data.Result {
		if cred.MatchExpression == matchExpression {
			return true, nil
		}
	}
	return false, nil
}

func createCredential(headers map[string]string, client httpClient.HttpClient, i *v1.Infinispan, ctx pipeline.Context) error {
	admin := ctx.ConfigFiles().AdminIdentities
	formData := map[string]string{
		"matchExpression": MatchExpression(i),
		"username":        admin.Username,
		"password":        admin.Password,
	}

	rsp, err := client.PostForm(CryostatCredentialsApi, headers, formData)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err := httpClient.ValidateResponse(rsp, err, "adding Cryostat credentials", http.StatusCreated); err != nil {
		return err
	}
	return nil
}

func MatchExpression(i *v1.Infinispan) string {
	return fmt.Sprintf("target.labels['infinispan_cr'] == '%s'", i.Name)
}
