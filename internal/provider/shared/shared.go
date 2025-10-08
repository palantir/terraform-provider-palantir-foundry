package shared

import (
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
)

type FoundryProviderData struct {
	Client *v2.ClientWithResponses
	Flags  *Flags
}

type Flags struct {
	DeletionsEnabled bool
}
