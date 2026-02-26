<p>
<a href="https://registry.terraform.io/providers/palantir/palantir-foundry/latest/docs"><img src="https://img.shields.io/badge/Terraform-docs-purple?logo=terraform" alt="Terraform Documentation"></a>
<a href="https://autorelease.general.dmz.palantir.tech/palantir/terraform-provider-palantir-foundry"><img align="right" src="https://img.shields.io/badge/Perform%20an-Autorelease-success.svg" alt="Autorelease"></a>
</p>


# Terraform Provider for Palantir Foundry

Manage your Palantir Foundry enrollment with Terraform. All functionality in this provider is available through the [Foundry REST API](https://www.palantir.com/docs/foundry/api/v2/).

## Requirements

- Terraform 1.0 or later
- A Palantir Foundry enrollment with a [Custom Application](https://www.palantir.com/docs/foundry/developer-console/overview/) configured for client-credential authentication

## Usage

Below is a minimal `main.tf` to configure this provider.

```hcl
# Declare the provider and version
terraform {
  required_providers {
    foundry = {
      source = "palantir/palantir-foundry"
      version = "your-specified-version-here"
    }
  }
}

# Initialize the provider
provider "foundry" {
    # may be specified with PLTR_CLIENT_ID environment variable
    client_id = "your-client-id-here"
    # may be specified with PLTR_CLIENT_SECRET environment variable            
    client_secret = "your-client-secret-here"
    # may be specified with PLTR_HOSTNAME environment variable or omitted entirely when running within Foundry
    host = "https://example.palantirfoundry.com/"
    # OPTIONAL - If true, disabled deletions for all resources, defaults to false
    deletions_disabled = false
}

# Configure a resource
resource "foundry_group" "example-group" {
  name        = "Example group name"
  description = "An example group created by Terraform"
  organizations = ["example-organization-rid"]
}
```

For a full list of supported options, resources and data sources, please refer to the [documentation](https://registry.terraform.io/providers/palantir/palantir-foundry/latest/docs).

## Contributing

While we encourage bug reports and feature requests as issues, we are not currently accepting code contributions to this repository.
Development of this provider happens internally at Palantir and changes are periodically released to this repository.
If you have an urgent need for a feature or bug fix, please reach out to your Palantir representative.
