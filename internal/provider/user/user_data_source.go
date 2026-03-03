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

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

var (
	_ datasource.DataSource              = &userDataSource{}
	_ datasource.DataSourceWithConfigure = &userDataSource{}
)

func NewUserDataSource() datasource.DataSource {
	return &userDataSource{}
}

type userDataSource struct {
	client *v2.ClientWithResponses
}

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading a Foundry User.",
		Attributes:  getUserSchemaAttributes(false),
	}
}

func (d *userDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *userDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data userModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := d.ReadUser(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the User", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *userDataSource) ReadUser(ctx context.Context, data *userModel) error {
	adminGetUserParams := v2.AdminGetUserParams{}
	userIDAsUuid, err := uuid.Parse(data.ID.ValueString())

	if err != nil {
		return fmt.Errorf("invalid UUID format for principal ID %s: %w", data.ID.ValueString(), err)
	}

	httpResp, err := d.client.AdminGetUser(ctx, userIDAsUuid, &adminGetUserParams)

	if err != nil {
		return fmt.Errorf("AdminGetUser request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminGetUser response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminGetUser: %w", err)
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

func getUserSchemaAttributes(readOnly bool) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Description: "ID of the User.",
			Computed:    readOnly,
			Required:    !readOnly,
		},
		"username": schema.StringAttribute{
			Description: "The Foundry username of the User. This is unique within the realm.",
			Computed:    true,
		},
		"email": schema.StringAttribute{
			Description: "The email at which to contact a User. Multiple users may have the same email address.",
			Computed:    true,
		},
		"given_name": schema.StringAttribute{
			Description: "The given name of the User.",
			Computed:    true,
		},
		"family_name": schema.StringAttribute{
			Description: "The family name (last name) of the User.",
			Computed:    true,
		},
		"realm": schema.StringAttribute{
			Description: "Realm of the User.",
			Computed:    true,
		},
		"organization": schema.StringAttribute{
			Description: "The RID of the user's primary Organization.",
			Computed:    true,
		},
	}
}
