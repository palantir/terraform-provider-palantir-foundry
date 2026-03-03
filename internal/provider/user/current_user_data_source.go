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

package user

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

var (
	_ datasource.DataSource              = &currentUserDataSource{}
	_ datasource.DataSourceWithConfigure = &currentUserDataSource{}
)

func NewCurrentUserDataSource() datasource.DataSource {
	return &currentUserDataSource{}
}

type currentUserDataSource struct {
	client *v2.ClientWithResponses
}

func (d *currentUserDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_current_user"
}

func (d *currentUserDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading information about the current authenticated Foundry User.",
		Attributes:  getUserSchemaAttributes(true),
	}
}

func (d *currentUserDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*shared.FoundryProviderData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *shared.FoundryProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = providerData.Client
}

func (d *currentUserDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data userModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := d.ReadCurrentUser(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the current User", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *currentUserDataSource) ReadCurrentUser(ctx context.Context, data *userModel) error {
	httpResp, err := d.client.AdminGetCurrentUser(ctx)

	if err != nil {
		return fmt.Errorf("AdminGetCurrentUser request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminGetCurrentUser response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminGetCurrentUser: %w", err)
	}

	var httpUserResponseBody userResponseBody
	if err := json.Unmarshal(bodyBytes, &httpUserResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	data.ID = types.StringValue(httpUserResponseBody.ID)
	data.Username = types.StringValue(httpUserResponseBody.Username)
	data.Email = helper.HandleOptionalString(httpUserResponseBody.Email)
	data.GivenName = helper.HandleOptionalString(httpUserResponseBody.GivenName)
	data.FamilyName = helper.HandleOptionalString(httpUserResponseBody.FamilyName)
	data.Realm = types.StringValue(httpUserResponseBody.Realm)
	data.Organization = helper.HandleOptionalString(httpUserResponseBody.Organization)

	return nil
}
