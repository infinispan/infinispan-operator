/*
Package client provides a client api that should be used to query and manipulate Infinispan server(s) using HTTP.

A new client should be created by calling the New factory method:

	var httpClient http.HttpClient
	...
	ispnClient := client.New(httpClient)

The api package defines all types and interfaces required to interact with the Infinispan server(s).

Version specific implementations of the api package should be created in their own sub-package using the scheme `v<major-version>`.
For example, the Infinispan 13.x client implementation resides in the `client/v13` package.

To prevent duplicated code, newer api implementation packages should use composition to use older implementations that still
function as expected on newer server versions.

For example, if v14 can reuse a new `api.Cache` implementation but requires a new `api.Caches` implementation:
	package v14
	...
	type infinispan struct {
		http.HttpClient
		ispn13 api.Infinispan
	}

	func New(client http.HttpClient) api.Infinispan {
		return &infinispan{
			HttpClient: client,
			ispn13: v13.New(client),
		}
	}

	func (i *infinispan) Cache(name string) api.Cache {
		return i.ispn13.Cache(name)
	}

	func (i *infinispan) Caches() api.Caches {
		return &caches{i.HttpClient}
	}
	...
*/
package client

import (
	"github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v13 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v13"
	v14 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
)

// New Factory to obtain Infinispan implementation
func New(operand version.Operand, client http.HttpClient) api.Infinispan {
	switch operand.UpstreamVersion.Major {
	case 13:
		return v13.New(client)
	default:
		return v14.New(client)
	}
}
