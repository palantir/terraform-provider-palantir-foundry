package services

import (
	"os"
	"testing"
)

func TestResolveUrlsServiceDiscovery(t *testing.T) {
	serviceDiscoveryFile, err := os.CreateTemp("", "service_discovery_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(serviceDiscoveryFile.Name())

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

	os.Setenv("FOUNDRY_SERVICE_DISCOVERY_V2", serviceDiscoveryFile.Name())
	defer os.Unsetenv("FOUNDRY_SERVICE_DISCOVERY_V2")

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
	os.Setenv("BASE_HOSTNAME", baseHostname)
	defer os.Unsetenv("BASE_HOSTNAME")

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
	os.Unsetenv("BASE_HOSTNAME")
	os.Unsetenv("FOUNDRY_SERVICE_DISCOVERY_V2")

	urls := ResolveUrls("")

	if urls != nil {
		t.Errorf("Expected nil URLs, got %v", urls)
	}
}
