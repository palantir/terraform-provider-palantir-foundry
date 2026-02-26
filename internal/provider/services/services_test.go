// Copyright 2025 Palantir Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package services

import (
	"os"
	"testing"

	"github.com/palantir/terraform-provider-palantir-foundry/internal/env"
)

func TestResolveUrlsServiceDiscovery(t *testing.T) {
	serviceDiscoveryFile, err := os.CreateTemp("", "service_discovery_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() {
		_ = os.Remove(serviceDiscoveryFile.Name())
	}()

	serviceDiscoveryContent := `
api_gateway:
    - https://in.cluster.service.local:8443/compute/jkhe23/foundry/api-gateway/api
multipass:
    - https://other.cluster.service.local:8443/compute/ad89721/multipass/api
`
	_, err = serviceDiscoveryFile.WriteString(serviceDiscoveryContent)
	if err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}

	_ = os.Setenv(env.FOUNDRY_SERVICE_DISCOVERY, serviceDiscoveryFile.Name())
	defer func() {
		_ = os.Unsetenv(env.FOUNDRY_SERVICE_DISCOVERY)
	}()

	urls := ResolveUrls("")

	expectedApiGatewayUrl := "https://in.cluster.service.local:8443/compute/jkhe23/foundry/api-gateway/api"
	expectedMultipassUrl := "https://other.cluster.service.local:8443/compute/ad89721/multipass/api"

	if urls.ApiGatewayUrl != expectedApiGatewayUrl {
		t.Errorf("Expected ApiGatewayUrl %s, got %s", expectedApiGatewayUrl, urls.ApiGatewayUrl)
	}
	if urls.MultipassUrl != expectedMultipassUrl {
		t.Errorf("Expected MultipassUrl %s, got %s", expectedMultipassUrl, urls.MultipassUrl)
	}
}

func TestResolveUrlsConfigHost(t *testing.T) {
	configHost := "https://custom.host/foundry/"

	urls := ResolveUrls(configHost)

	expectedApiGatewayUrl := "https://custom.host/foundry/api"
	expectedMultipassUrl := "https://custom.host/foundry/multipass/api"

	if urls.ApiGatewayUrl != expectedApiGatewayUrl {
		t.Errorf("Expected ApiGatewayUrl %s, got %s", expectedApiGatewayUrl, urls.ApiGatewayUrl)
	}
	if urls.MultipassUrl != expectedMultipassUrl {
		t.Errorf("Expected MultipassUrl %s, got %s", expectedMultipassUrl, urls.MultipassUrl)
	}
}

func TestResolveUrlsEnvBaseHostname(t *testing.T) {
	baseHostname := "https://env.host/foundry/"
	_ = os.Setenv(env.HOSTNAME_ENV_VAR, baseHostname)
	defer func() {
		_ = os.Unsetenv(env.HOSTNAME_ENV_VAR)
	}()

	urls := ResolveUrls("")

	expectedApiGatewayUrl := "https://env.host/foundry/api"
	expectedMultipassUrl := "https://env.host/foundry/multipass/api"

	if urls.ApiGatewayUrl != expectedApiGatewayUrl {
		t.Errorf("Expected ApiGatewayUrl %s, got %s", expectedApiGatewayUrl, urls.ApiGatewayUrl)
	}
	if urls.MultipassUrl != expectedMultipassUrl {
		t.Errorf("Expected MultipassUrl %s, got %s", expectedMultipassUrl, urls.MultipassUrl)
	}
}

func TestResolveUrlsNoConfig(t *testing.T) {
	_ = os.Unsetenv(env.HOSTNAME_ENV_VAR)
	_ = os.Unsetenv(env.FOUNDRY_SERVICE_DISCOVERY)

	urls := ResolveUrls("")

	if urls != nil {
		t.Errorf("Expected nil URLs, got %v", urls)
	}
}
