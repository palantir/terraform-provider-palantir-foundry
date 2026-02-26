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

package enrollment

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
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/constants"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

var (
	_ datasource.DataSource              = &enrollmentDataSource{}
	_ datasource.DataSourceWithConfigure = &enrollmentDataSource{}
)

func NewEnrollmentDataSource() datasource.DataSource {
	return &enrollmentDataSource{}
}

type enrollmentDataSource struct {
	client *v2.ClientWithResponses
}

type enrollmentDataSourceModel struct {
	RID  types.String `tfsdk:"rid"`
	Name types.String `tfsdk:"name"`
}

func (d *enrollmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enrollment"
}

func (d *enrollmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Data source for reading a Foundry Enrollment.",
		Attributes: map[string]schema.Attribute{
			"rid": schema.StringAttribute{
				Description: "RID of the Enrollment.",
				Required:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the Enrollment.",
				Computed:    true,
			},
		},
	}
}

func (d *enrollmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *enrollmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data enrollmentDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := d.ReadEnrollment(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Enrollment", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (d *enrollmentDataSource) ReadEnrollment(ctx context.Context, data *enrollmentDataSourceModel) error {
	previewMode := constants.PreviewMode
	adminGetEnrollmentParams := v2.AdminGetEnrollmentParams{Preview: &previewMode}
	httpResp, err := d.client.AdminGetEnrollment(ctx, data.RID.ValueString(), &adminGetEnrollmentParams)

	if err != nil {
		return fmt.Errorf("AdminGetEnrollment request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminGetEnrollment response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminGetEnrollment: %w", err)
	}

	var httpEnrollmentResponseBody enrollmentResponseBody
	if err := json.Unmarshal(bodyBytes, &httpEnrollmentResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	data.Name = types.StringValue(httpEnrollmentResponseBody.Name)

	return nil
}
