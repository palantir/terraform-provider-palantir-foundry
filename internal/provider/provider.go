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

package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/securityprovider"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/auth"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/enrollment"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/group"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/marking"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/organization"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/project"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/space"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ provider.Provider = &FoundryProvider{}
)

// FoundryProvider is the provider implementation.
type FoundryProvider struct{}

// New creates a new provider instance
func New() provider.Provider {
	return &FoundryProvider{}
}

// Metadata returns the provider type name.
func (p *FoundryProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "foundry"
}

// Schema defines the provider-level schema for configuration data.
func (p *FoundryProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Interact with Foundry.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional: true,
			},
			"client_id": schema.StringAttribute{
				Optional: true,
			},
			"client_secret": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

type foundryProviderModel struct {
	Host         types.String `tfsdk:"host"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

// Configure prepares a Foundry API client for data sources and resources.
func (p *FoundryProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config foundryProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Foundry API Host", "Please provide the API host for Foundry in the provider configuration.")
	}

	if config.ClientID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("client_id"),
			"Unknown Foundry API Client ID", "Please provide the API Client ID for Foundry in the provider configuration.")
	}

	if config.ClientSecret.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("client_secret"),
			"Unknown Foundry API Client Secret", "Please provide the API Client Secret for Foundry in the provider configuration.")
	}

	if resp.Diagnostics.HasError() {
		return
	}

	// Default values to environment variables, but override
	// with Terraform configuration value if set.

	host := os.Getenv("BASE_HOSTNAME")
	clientID := os.Getenv("CLIENT_ID")
	clientSecret := os.Getenv("CLIENT_SECRET")

	if !config.Host.IsNull() {
		host = config.Host.ValueString()
	}

	if !config.ClientID.IsNull() {
		clientID = config.ClientID.ValueString()
	}

	if !config.ClientSecret.IsNull() {
		clientSecret = config.ClientSecret.ValueString()
	}

	// If any of the expected configurations are missing, return
	// errors with provider-specific guidance.

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Foundry API Host", "Please provide the API host for Foundry in the provider configuration.")
	}

	if clientID == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Foundry API clientID", "Please provide the API clientID for Foundry in the provider configuration.")
	}

	if clientSecret == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Foundry API clientSecret", "Please provide the API clientSecret for Foundry in the provider configuration.")
	}

	if resp.Diagnostics.HasError() {
		return
	}

	tokenString, err := auth.GetAuthToken(host, clientID, clientSecret)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to retrieve Foundry API Token",
			"Unable to retrieve Foundry API Token: "+err.Error(),
		)
	}

	token, err := securityprovider.NewSecurityProviderBearerToken(tokenString)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to convert Foundry API Token",
			"Unable to convert Foundry API Token: "+err.Error(),
		)
		return
	}
	client, err := v2.NewClientWithResponses(host+"api", v2.WithRequestEditorFn(token.Intercept))

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Foundry API Client",
			"Unable to create Foundry API Client: "+err.Error(),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

// DataSources defines the data sources implemented in the provider.
func (p *FoundryProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// Resources defines the resources implemented in the provider.
func (p *FoundryProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		enrollment.NewEnrollmentResource,
		group.NewGroupResource,
		marking.NewMarkingResource,
		organization.NewOrganizationResource,
		project.NewProjectResource,
		space.NewSpaceResource,
	}
}
