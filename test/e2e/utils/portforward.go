package utils

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForward returns a stop and ready channel
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
			fmt.Printf("error forwarding port: %s\n", err.Error())
		}
	}()

	// Wait for readyChan before returning
	for range readyChan {
	}
	if len(errOut.String()) != 0 {
		return nil, fmt.Errorf("unable to port-forward: %s", errOut.String())
	} else if len(out.String()) != 0 {
		fmt.Println(out.String())
	}
	return stopChan, nil
}
