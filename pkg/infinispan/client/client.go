/*
Package client provides a client api that should be used to query and manipulate Infinispan server(s) using HTTP.

A new client should be created by calling the New factory method:

	var httpClient http.HttpClient
	...
	ispnClient := client.New(httpClient)

The api package defines all types and interfaces required to interact with the Infinispan server(s).

Version specific implementations of the api package should be created in their own sub-package using the scheme `v<major-version>`.
For example, the Infinispan 14.x client implementation resides in the `client/v14` package.

To prevent duplicated code, newer api implementation packages should use composition to use older implementations that still
function as expected on newer server versions.

For example, if v15 can reuse a new `api.Cache` implementation but requires a new `api.Caches` implementation:

	package v15
	...
	type infinispan struct {
		http.HttpClient
		ispn14 api.Infinispan
	}

	func New(client http.HttpClient) api.Infinispan {
		return &infinispan{
			HttpClient: client,
			ispn14: v14.New(client),
		}
	}

	func (i *infinispan) Cache(name string) api.Cache {
		return i.ispn14.Cache(name)
	}

	func (i *infinispan) Caches() api.Caches {
		return &caches{i.HttpClient}
	}
	...
*/
package client

import (
	"fmt"
	"regexp"

	"github.com/blang/semver"
	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v14 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
	v15 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v15"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
)

// New Factory to obtain Infinispan implementation
func New(operand version.Operand, client httpClient.HttpClient) api.Infinispan {
	return ispnClient(operand.UpstreamVersion.Major, client)
}

func NewUnknownVersion(client httpClient.HttpClient) (api.Infinispan, error) {
	wrapError := func(e error) error {
		return fmt.Errorf("unable to determine server version: %w", e)
	}
	info, err := v15.New(client).Container().Info()
	if err != nil {
		info, err = v14.New(client).Container().Info()
		if err != nil {
			return nil, wrapError(err)
		}
	}
	re := regexp.MustCompile(`\d+(\.\d+){2,}`)
	versionStr := re.FindStringSubmatch(info.Version)
	_version, err := semver.Parse(versionStr[0])
	if err != nil {
		return nil, wrapError(fmt.Errorf("unable to parse server version: %w", err))
	}
	return ispnClient(_version.Major, client), nil
}

func ispnClient(majorVersion uint64, client httpClient.HttpClient) api.Infinispan {
	switch majorVersion {
	// We must still return a client for the dropped Infinispan 13 so that we can interact with the server when upgrading
	// to a supported version. The only difference between the 13 and 14 client was the Cache EqualConfiguration implementation
	// which is not required for upgrades.
	case 13, 14:
		return v14.New(client)
	default:
		return v15.New(client)
	}
}
