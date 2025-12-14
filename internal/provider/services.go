package provider

import (
	"os"

	"gopkg.in/yaml.v2"
)

type ResolvedServiceUrls struct {
	ApiGatewayUrl string
	MultipassUrl  string
}

type serviceDiscovery struct {
	Multipass  []string `yaml:"multipass"`
	ApiGateway []string `yaml:"api_gateway"`
}

func ResolveUrls(config foundryProviderModel) *ResolvedServiceUrls {
	host := config.Host.ValueString()
	if host == "" {
		host = os.Getenv("BASE_HOSTNAME")
	}

	if host != "" {
		return &ResolvedServiceUrls{
			ApiGatewayUrl: host + "api",
			MultipassUrl:  host + "multipass/api",
		}
	}

	serviceDiscoveryPath := os.Getenv("FOUNDRY_SERVICE_DISCOVERY_V2")
	if serviceDiscoveryPath != "" {
		yamlFile, err := os.ReadFile(serviceDiscoveryPath)
		if err != nil {
			panic(err)
		}
		var config serviceDiscovery
		err = yaml.Unmarshal(yamlFile, &config)
		if err != nil {
			panic(err)
		}

		return &ResolvedServiceUrls{
			ApiGatewayUrl: config.ApiGateway[0],
			MultipassUrl:  config.Multipass[0],
		}
	}

	return nil
}
