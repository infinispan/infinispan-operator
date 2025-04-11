package utils

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForward returns a stop channel that should be closed when port-forwarding is no longer required
func (k TestKubernetes) PortForward(pod, namespace string, ports []string) (chan struct{}, error) {
	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)

	config := k.Kubernetes.RestConfig
	roundTripper, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return stopChan, err
	}

	dialer := spdy.NewDialer(
		upgrader,
		&http.Client{
			Transport: roundTripper,
		},
		http.MethodPost,
		&url.URL{
			Scheme: "https",
			Path:   fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, pod),
			Host:   strings.TrimLeft(config.Host, "htps:/"),
		},
	)

	out, errOut := new(bytes.Buffer), new(bytes.Buffer)
	forwarder, err := portforward.New(dialer, ports, stopChan, readyChan, out, errOut)
	if err != nil {
		return stopChan, err
	}

	go func() {
		if err = forwarder.ForwardPorts(); err != nil {
			Log().Warnf("error forwarding port: %s", err.Error())
		}
	}()

	// Wait for readyChan before returning
	select {
	case <-readyChan:
		return stopChan, nil
	case <-time.After(10 * time.Second):
		return stopChan, fmt.Errorf("portforward timedout for '%s:%s'. stdErr='%s', stdOut='%s'", pod, namespace, errOut.String(), out.String())
	}
}
