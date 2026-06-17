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

package usage_account

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/constants"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource              = &usageAccountResource{}
	_ resource.ResourceWithConfigure = &usageAccountResource{}
)

// NewUsageAccountResource is a helper function to simplify provider implementation.
func NewUsageAccountResource() resource.Resource {
	return &usageAccountResource{}
}

// usageAccountResource is the resource implementation.
type usageAccountResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider data to the resource.
func (r *usageAccountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*shared.FoundryProviderData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected shared.FoundryProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = providerData.Client
	r.deletionsDisabled = providerData.Flags.DeletionsDisabled
}

// Metadata returns the resource type name.
func (r *usageAccountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_usage_account"
}

// Schema defines the schema for the resource.
func (r *usageAccountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Usage Account.",
		Attributes: map[string]schema.Attribute{
			"rid": schema.StringAttribute{
				Description: "RID of the Usage Account.",
				Computed:    true,
			},
			"enrollment_rid": schema.StringAttribute{
				Description: "The RID of the Enrollment this Usage Account belongs to. This field is required if the resource is created within Terraform, but not necessarily if created outside of Terraform and imported.",
				Optional:    true,
			},
			"display_name": schema.StringAttribute{
				Description: "Display name of the Usage Account.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the Usage Account.",
				Optional:    true,
			},
			"internal": schema.BoolAttribute{
				Description: "Whether this Usage Account is internal. Internal Usage Accounts are system-managed and cannot be modified or deleted.",
				Computed:    true,
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *usageAccountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan usageAccountResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.CreateUsageAccount(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the Usage Account. Please fix your plan if needed and re-apply.", err.Error())
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *usageAccountResource) CreateUsageAccount(ctx context.Context, resp *resource.CreateResponse, plan *usageAccountResourceModel) error {
	if plan.EnrollmentRID.IsNull() || plan.EnrollmentRID.IsUnknown() || plan.EnrollmentRID.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Missing required attribute",
			"enrollment_rid is required when creating a new Usage Account. It may be omitted only when importing an existing resource.",
		)
		return fmt.Errorf("enrollment_rid is required when creating a new Usage Account")
	}

	previewMode := constants.PreviewMode
	params := v2.ResourceManagementCreateUsageAccountParams{Preview: &previewMode}

	description := plan.Description.ValueString()
	requestBody := v2.ResourceManagementCreateUsageAccountJSONRequestBody{
		DisplayName:   plan.DisplayName.ValueString(),
		EnrollmentRid: plan.EnrollmentRID.ValueString(),
		Description:   &description,
	}
	jsonBytes, _ := json.Marshal(requestBody)
	tflog.Info(ctx, string(jsonBytes))

	httpResp, err := r.client.ResourceManagementCreateUsageAccount(ctx, &params, requestBody)

	if err != nil {
		resp.Diagnostics.AddError("ResourceManagementCreateUsageAccount request failed", err.Error())
		return fmt.Errorf("ResourceManagementCreateUsageAccount request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from ResourceManagementCreateUsageAccount response", err.Error())
			return fmt.Errorf("failed to format error logging from ResourceManagementCreateUsageAccount response: %w", err)
		}
		resp.Diagnostics.AddError("Response from ResourceManagementCreateUsageAccount was unsuccessful: ", returnString)
		return fmt.Errorf("response from ResourceManagementCreateUsageAccount was unsuccessful: %s", returnString)
	}

	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from ResourceManagementCreateUsageAccount", err.Error())
		return fmt.Errorf("failed to parse response from ResourceManagementCreateUsageAccount: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	if httpResponseBody.RID == "" {
		resp.Diagnostics.AddError("RID returned as empty",
			"RID was not populated in response, "+
				"so Terraform best practice is NOT to update state as resource likely was not properly created")
		return fmt.Errorf("RID returned as empty: %s", httpResponseBody.RID)
	}

	plan.RID = types.StringValue(httpResponseBody.RID)
	plan.Internal = types.BoolValue(httpResponseBody.Internal)
	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *usageAccountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state usageAccountResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadUsageAccount(ctx, resp, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Usage Account", err.Error())
		return
	}

	if resp.Diagnostics.Warnings().Contains(providerError.ResourceNotFoundWarning(state.RID.ValueString(), "Usage Account")) {
		resp.State.RemoveResource(ctx)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *usageAccountResource) ReadUsageAccount(ctx context.Context, resp *resource.ReadResponse, state *usageAccountResourceModel) error {
	previewMode := constants.PreviewMode
	params := v2.ResourceManagementGetUsageAccountParams{Preview: &previewMode}
	httpResp, err := r.client.ResourceManagementGetUsageAccount(ctx, state.RID.ValueString(), &params)

	if err != nil {
		resp.Diagnostics.AddError("ResourceManagementGetUsageAccount request failed", err.Error())
		return fmt.Errorf("ResourceManagementGetUsageAccount request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusNotFound {
			resp.Diagnostics.Append(providerError.ResourceNotFoundWarning(state.RID.ValueString(), "Usage Account"))
			return nil
		}
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from ResourceManagementGetUsageAccount response", err.Error())
			return fmt.Errorf("failed to format error logging from ResourceManagementGetUsageAccount response: %w", err)
		}
		resp.Diagnostics.AddError("Response from ResourceManagementGetUsageAccount was unsuccessful: ", returnString)
		return fmt.Errorf("response from ResourceManagementGetUsageAccount was unsuccessful: %s", returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from ResourceManagementGetUsageAccount", err.Error())
		return fmt.Errorf("failed to parse response from ResourceManagementGetUsageAccount: %w", err)
	}

	var httpResponseBody responseBody
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	state.RID = types.StringValue(httpResponseBody.RID)
	state.EnrollmentRID = types.StringValue(httpResponseBody.EnrollmentRID)
	state.DisplayName = types.StringValue(httpResponseBody.DisplayName)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)
	state.Internal = types.BoolValue(httpResponseBody.Internal)
	return nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *usageAccountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan usageAccountResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state usageAccountResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateUsageAccount(ctx, resp, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Usage Account. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *usageAccountResource) UpdateUsageAccount(ctx context.Context, resp *resource.UpdateResponse, plan *usageAccountResourceModel, state *usageAccountResourceModel) error {
	if !plan.EnrollmentRID.Equal(state.EnrollmentRID) {
		return fmt.Errorf("you may not change the enrollment_rid of a Usage Account once it has been created. Please revert your plan to the existing enrollment_rid and re-apply")
	}

	previewMode := constants.PreviewMode
	params := v2.ResourceManagementReplaceUsageAccountParams{Preview: &previewMode}

	httpResp, err := r.client.ResourceManagementReplaceUsageAccount(ctx, state.RID.ValueString(), &params, v2.ResourceManagementReplaceUsageAccountJSONRequestBody{
		DisplayName: plan.DisplayName.ValueString(),
		Description: plan.Description.ValueStringPointer(),
	})

	if err != nil {
		resp.Diagnostics.AddError("ResourceManagementReplaceUsageAccount request failed", err.Error())
		return fmt.Errorf("ResourceManagementReplaceUsageAccount request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from ResourceManagementReplaceUsageAccount response", err.Error())
			return fmt.Errorf("failed to format error logging from ResourceManagementReplaceUsageAccount response: %w", err)
		}
		resp.Diagnostics.AddError("Response from ResourceManagementReplaceUsageAccount was unsuccessful: ", returnString)
		return fmt.Errorf("response from ResourceManagementReplaceUsageAccount was unsuccessful: %s", returnString)
	}

	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from ResourceManagementReplaceUsageAccount", err.Error())
		return fmt.Errorf("failed to parse response from ResourceManagementReplaceUsageAccount: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	state.DisplayName = types.StringValue(httpResponseBody.DisplayName)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)
	state.Internal = types.BoolValue(httpResponseBody.Internal)
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *usageAccountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state usageAccountResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If deletions are disabled, do not delete the remote Usage Account but remove the resource from state.
	if r.deletionsDisabled {
		resp.Diagnostics.AddWarning("Tried to perform a deletion when the deletions_disabled flag was set to true.",
			fmt.Sprintf("Remote Usage Account with display name %s and rid %s will not be deleted, but this resource will be removed from state.", state.DisplayName.ValueString(), state.RID.ValueString()))
		return
	}

	previewMode := constants.PreviewMode
	params := v2.ResourceManagementDeleteUsageAccountParams{Preview: &previewMode}

	httpResp, err := r.client.ResourceManagementDeleteUsageAccount(ctx, state.RID.ValueString(), &params)

	if err != nil {
		resp.Diagnostics.AddError("ResourceManagementDeleteUsageAccount request failed", err.Error())
		return
	}

	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("failed to format error logging from ResourceManagementDeleteUsageAccount response", err.Error())
		}
		resp.Diagnostics.AddError("ResourceManagementDeleteUsageAccount request failed", returnString)
		return
	}
}

// ImportState imports an existing usage account into Terraform state.
func (r *usageAccountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	usageAccountRID := req.ID

	if usageAccountRID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the Usage Account RID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing Usage Account with RID %s", usageAccountRID))

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rid"), usageAccountRID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the RID
}
