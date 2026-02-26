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

package marking

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	_ resource.Resource              = &markingCategoryResource{}
	_ resource.ResourceWithConfigure = &markingCategoryResource{}
)

// NewMarkingCategoryResource is a helper function to simplify provider implementation.
func NewMarkingCategoryResource() resource.Resource {
	return &markingCategoryResource{}
}

// markingCategoryResource is the resource implementation.
type markingCategoryResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider data to the resource.
func (r *markingCategoryResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *markingCategoryResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_marking_category"
}

// Schema defines the schema for the resource.
func (r *markingCategoryResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Marking Category.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "ID of the Marking Category. For user-created categories, this will be a UUID.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the Marking Category.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the Marking Category.",
				Required:    true,
			},
			"category_type": schema.StringAttribute{
				Description: "The type of the Marking Category. Either CONJUNCTIVE or DISJUNCTIVE. " +
					"CONJUNCTIVE (and) means a user must have access to all of markings from the category which are applied to a resource to access that resource. " +
					"DISJUNCTIVE (or) means a user must have access to at least one marking from the category which are applied to a resource to access that resource. " +
					"All user-created categories will be CONJUNCTIVE.",
				Computed: true,
			},
			"marking_type": schema.StringAttribute{
				Description: "The type of markings in this category. Either CBAC (classification-based access control) or MANDATORY. All user-created categories will be MANDATORY.",
				Computed:    true,
			},
			"created_by": schema.StringAttribute{
				Description: "The ID of the user who created this Marking Category.",
				Computed:    true,
			},
			"created_time": schema.StringAttribute{
				Description: "The time at which this Marking Category was created.",
				Computed:    true,
			},
			"initial_permissions": schema.SingleNestedAttribute{
				Description: "The initial permissions to be applied when creating the Marking Category. " +
					"Any changes to this field after Marking Category creation will not be applied.",
				Required: true,
				Attributes: map[string]schema.Attribute{
					"is_public": schema.BoolAttribute{
						Description: "If true, all users who are members of at least one of the Organizations from organization_rids " +
							"can view the Markings in the category. If false, only users who are explicitly granted the VIEW role can view the Markings in the category.",
						Required: true,
					},
					"organization_rids": schema.SetAttribute{
						Description: "The RIDs of the organizations that have access to this Marking Category.",
						Required:    true,
						ElementType: types.StringType,
					},
					"roles": schema.SetNestedAttribute{
						Description: "The initial set of Role Assignments to be applied when creating the Marking Category. " +
							"At least one role assignment with the ADMINISTER role must be provided. " +
							"The following Roles can be assigned: ADMINISTER (can manage the category) or VIEW (can view markings in the category).",
						Required: true,
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"role": schema.StringAttribute{
									Description: "The role to assign. Must be either ADMINISTER or VIEW.",
									Required:    true,
									Validators: []validator.String{
										stringvalidator.OneOf("ADMINISTER", "VIEW"),
									},
								},
								"principal_id": schema.StringAttribute{
									Description: "The ID of the principal (user or group) to assign the role to.",
									Required:    true,
								},
							},
						},
					},
				},
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *markingCategoryResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan markingCategoryResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.CreateMarkingCategory(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the Marking Category. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingCategoryResource) CreateMarkingCategory(ctx context.Context, resp *resource.CreateResponse, plan *markingCategoryResourceModel) error {
	if plan.InitialPermissions == nil {
		return fmt.Errorf("initial_permissions is required")
	}

	// Convert organization RIDs to []string
	var organizationRids []string
	diags := plan.InitialPermissions.OrganizationRids.ElementsAs(ctx, &organizationRids, false)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return fmt.Errorf("failed to convert organization_rids to Go slice")
	}

	// Convert roles to API format
	var roleAssignments []markingCategoryRoleAssignment
	diags = plan.InitialPermissions.Roles.ElementsAs(ctx, &roleAssignments, false)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return fmt.Errorf("failed to convert roles to Go slice")
	}

	if len(roleAssignments) == 0 {
		return fmt.Errorf("initial_permissions.roles must contain at least one role assignment")
	}

	var apiRoleAssignments []v2.AdminMarkingCategoryRoleAssignment
	for _, role := range roleAssignments {
		principalIDAsUUID, err := uuid.Parse(role.PrincipalID)
		if err != nil {
			return fmt.Errorf("invalid UUID format for principal ID %s: %w", role.PrincipalID, err)
		}

		apiRoleAssignments = append(apiRoleAssignments, v2.AdminMarkingCategoryRoleAssignment{
			Role:        v2.AdminMarkingCategoryRole(role.Role),
			PrincipalID: principalIDAsUUID,
		})
	}

	// Build the permissions body
	permissionsBody := v2.AdminMarkingCategoryPermissions{
		IsPublic:         plan.InitialPermissions.IsPublic.ValueBool(),
		OrganizationRids: &organizationRids,
		Roles:            &apiRoleAssignments,
	}

	previewMode := constants.PreviewMode
	httpResp, err := r.client.AdminCreateMarkingCategory(ctx,
		&v2.AdminCreateMarkingCategoryParams{
			Preview: &previewMode,
		},
		v2.AdminCreateMarkingCategoryJSONRequestBody{
			Name:               plan.Name.ValueString(),
			Description:        plan.Description.ValueString(),
			InitialPermissions: permissionsBody,
		})

	if err != nil {
		resp.Diagnostics.AddError("AdminCreateMarkingCategory request failed", err.Error())
		return fmt.Errorf("AdminCreateMarkingCategory request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminCreateMarkingCategory response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminCreateMarkingCategory response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminCreateMarkingCategory was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminCreateMarkingCategory was unsuccessful: %s", returnString)
	}

	// Parse the response body
	var httpResponseBody v2.AdminMarkingCategory

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminCreateMarkingCategory", err.Error())
		return fmt.Errorf("failed to parse response from AdminCreateMarkingCategory: %w", err)
	}
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	// CREATE - do not save state if id is not saved
	if httpResponseBody.ID == "" {
		tflog.Error(ctx, "ID was not populated in response, "+
			"so Terraform best practice is NOT to update state as resource likely was not properly created")
		resp.Diagnostics.AddError("ID returned as empty",
			"ID was not populated in response, "+
				"so Terraform best practice is NOT to update state as resource likely was not properly created")
		return fmt.Errorf("ID returned as empty: %s", httpResponseBody.ID)
	}

	// Set computed values
	plan.ID = types.StringValue(httpResponseBody.ID)
	plan.MarkingType = types.StringValue(string(httpResponseBody.MarkingType))
	plan.CategoryType = types.StringValue(string(httpResponseBody.CategoryType))

	if httpResponseBody.CreatedBy != nil {
		plan.CreatedBy = types.StringValue(httpResponseBody.CreatedBy.String())
	} else {
		plan.CreatedBy = types.StringNull()
	}

	plan.CreatedTime = types.StringValue(httpResponseBody.CreatedTime.String())

	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *markingCategoryResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state markingCategoryResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadMarkingCategory(ctx, resp, &state, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Marking Category", err.Error())
		return
	}

	if resp.Diagnostics.Warnings().Contains(providerError.ResourceNotFoundWarning(state.ID.ValueString(), "MarkingCategory")) {
		resp.State.RemoveResource(ctx)
		return
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingCategoryResource) ReadMarkingCategory(ctx context.Context, resp *resource.ReadResponse, state *markingCategoryResourceModel, markingCategoryID string) error {
	previewMode := constants.PreviewMode
	httpResp, err := r.client.AdminGetMarkingCategory(ctx, markingCategoryID, &v2.AdminGetMarkingCategoryParams{
		Preview: &previewMode,
	})

	if err != nil {
		resp.Diagnostics.AddError("AdminGetMarkingCategory request failed", err.Error())
		return fmt.Errorf("AdminGetMarkingCategory request failed: %w", err)
	}
	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusNotFound {
			resp.Diagnostics.Append(providerError.ResourceNotFoundWarning(state.ID.ValueString(), "MarkingCategory"))
			return nil
		}
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminGetMarkingCategory response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminGetMarkingCategory response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminGetMarkingCategory was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminGetMarkingCategory was unsuccessful: %s", returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminGetMarkingCategory", err.Error())
		return fmt.Errorf("failed to parse response from AdminGetMarkingCategory: %w", err)
	}

	// Parse response
	var httpResponseBody v2.AdminMarkingCategory
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	state.ID = types.StringValue(httpResponseBody.ID)
	state.Name = types.StringValue(httpResponseBody.Name)
	state.Description = types.StringValue(httpResponseBody.Description)
	state.CategoryType = types.StringValue(string(httpResponseBody.CategoryType))
	state.MarkingType = types.StringValue(string(httpResponseBody.MarkingType))

	if httpResponseBody.CreatedBy != nil {
		state.CreatedBy = types.StringValue(httpResponseBody.CreatedBy.String())
	} else {
		state.CreatedBy = types.StringNull()
	}

	state.CreatedTime = types.StringValue(httpResponseBody.CreatedTime.String())

	return nil
}

func (r *markingCategoryResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan markingCategoryResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state markingCategoryResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if initial_permissions changed
	planPermissionsChanged := false
	if plan.InitialPermissions != nil && state.InitialPermissions != nil {
		if !plan.InitialPermissions.IsPublic.Equal(state.InitialPermissions.IsPublic) ||
			!plan.InitialPermissions.OrganizationRids.Equal(state.InitialPermissions.OrganizationRids) ||
			!plan.InitialPermissions.Roles.Equal(state.InitialPermissions.Roles) {
			planPermissionsChanged = true
		}
	}

	if planPermissionsChanged {
		resp.Diagnostics.AddError("Initial Permissions cannot be updated after creation. Any changes will not be applied.",
			"Initial Permissions cannot be updated after creation. Any changes will not be applied.")
	}

	err := r.UpdateMarkingCategory(ctx, resp, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating marking category. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingCategoryResource) UpdateMarkingCategory(ctx context.Context, resp *resource.UpdateResponse, plan *markingCategoryResourceModel, state *markingCategoryResourceModel) error {
	previewMode := constants.PreviewMode
	httpResp, err := r.client.AdminReplaceMarkingCategory(ctx, state.ID.ValueString(),
		&v2.AdminReplaceMarkingCategoryParams{
			Preview: &previewMode,
		},
		v2.AdminReplaceMarkingCategoryJSONRequestBody{
			Name:        plan.Name.ValueString(),
			Description: plan.Description.ValueString(),
		})

	if err != nil {
		resp.Diagnostics.AddError("AdminReplaceMarkingCategory request failed", err.Error())
		return fmt.Errorf("AdminReplaceMarkingCategory request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminReplaceMarkingCategory response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminReplaceMarkingCategory response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminReplaceMarkingCategory was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminReplaceMarkingCategory was unsuccessful: %s", returnString)
	}

	// Read body and then close
	var httpResponseBody v2.AdminMarkingCategory

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminReplaceMarkingCategory", err.Error())
		return fmt.Errorf("failed to parse response from AdminReplaceMarkingCategory: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	// Update the state with the new values
	state.Name = types.StringValue(httpResponseBody.Name)
	state.Description = types.StringValue(httpResponseBody.Description)

	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *markingCategoryResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.deletionsDisabled {
		resp.Diagnostics.AddWarning("Marking Categories cannot be deleted",
			"Foundry does not support deleted marking categories. Since deletions_disabled is set to true, the remote marking category will not be deleted, but this resource will be removed from state.")
		return
	}

	// error here IMMEDIATELY, as Marking Categories are not allowed to be deleted
	tflog.Error(ctx, "Marking Categories cannot be deleted")
	resp.Diagnostics.AddError("Marking Categories cannot be deleted",
		"Foundry does not support deleted marking categories!")
}

// ImportState imports an existing marking category into Terraform state.
func (r *markingCategoryResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	markingCategoryID := req.ID

	if markingCategoryID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the marking category ID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing marking category with ID %s", markingCategoryID))

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), markingCategoryID)...)

	// Set initial_permissions to placeholder values since they cannot be read from the API
	// The user will need to ensure their config matches the actual permissions
	emptyOrgRids, diags := types.SetValueFrom(ctx, types.StringType, []string{})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create empty roles set with the correct element type
	rolesAttrTypes := map[string]attr.Type{
		"role":         types.StringType,
		"principal_id": types.StringType,
	}
	emptyRoles, diags := types.SetValue(types.ObjectType{AttrTypes: rolesAttrTypes}, []attr.Value{})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	initialPermissions := &markingCategoryInitialPermissionsModel{
		IsPublic:         types.BoolValue(false),
		OrganizationRids: emptyOrgRids,
		Roles:            emptyRoles,
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("initial_permissions"), initialPermissions)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the ID
}
