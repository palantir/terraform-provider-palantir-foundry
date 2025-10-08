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
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource              = &markingResource{}
	_ resource.ResourceWithConfigure = &markingResource{}
)

// NewMarkingResource is a helper function to simplify provider implementation.
func NewMarkingResource() resource.Resource {
	return &markingResource{}
}

// markingResource is the resource implementation.
type markingResource struct {
	client *v2.ClientWithResponses
}

// Configure adds the provider data to the resource.
func (r *markingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
}

// Metadata returns the resource type name.
func (r *markingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_marking"
}

// Schema defines the schema for the resource.
func (r *markingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry marking.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "ID of the Marking.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the marking.",
				Required:    true,
			},
			"category_id": schema.StringAttribute{
				Description: "The ID of a Marking Category. For user-created Categories, this will be a UUID. Markings associated with Organizations are placed in a category with ID \"Organization\". This field is immutable after creation.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the marking.",
				Optional:    true,
			},
			"marking_members": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "List of the IDs of the members (Users or Groups) of this Marking.",
				Optional:    true,
			},
			"marking_roles": schema.SetAttribute{
				Description: "List of role assignments for this Marking.",
				Optional:    true,
				ElementType: types.ObjectType{
					AttrTypes: map[string]attr.Type{
						"role":         types.StringType,
						"principal_id": types.StringType,
					},
				},
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *markingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan markingResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	err := r.CreateMarking(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the Marking. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingResource) CreateMarking(ctx context.Context, resp *resource.CreateResponse, plan *markingResourceModel) error {
	var initialRoleAssignments []RolesRequestBodyEntry
	diags := plan.MarkingRoles.ElementsAs(context.Background(), &initialRoleAssignments, false)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return fmt.Errorf("failed to convert roles to Go slice")
	}

	var initialRoleAssignmentsBody []v2.AdminMarkingRoleUpdate
	for _, item := range initialRoleAssignments {
		initialRoleAssignmentsBody = append(initialRoleAssignmentsBody, v2.AdminMarkingRoleUpdate{
			Role:        v2.AdminMarkingRole(item.Role),
			PrincipalID: item.PrincipalID,
		})
	}

	var initialMembers []string
	plan.MarkingMembers.ElementsAs(context.Background(), &initialMembers, false)

	previewMode := constants.PreviewMode
	adminCreateMarkingParams := v2.AdminCreateMarkingParams{Preview: &previewMode}
	description := plan.Description.ValueString()

	httpResp, err := r.client.AdminCreateMarking(ctx,
		&adminCreateMarkingParams,
		v2.AdminCreateMarkingJSONRequestBody{
			Name:                   plan.Name.ValueString(),
			CategoryID:             plan.CategoryID.ValueString(),
			Description:            &description,
			InitialMembers:         &initialMembers,
			InitialRoleAssignments: &initialRoleAssignmentsBody,
		})

	if err != nil {
		resp.Diagnostics.AddError("AdminCreateMarking request failed", err.Error())
		return fmt.Errorf("AdminCreateMarking request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminCreateMarking response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminCreateMarking response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminCreateMarking was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminCreateMarking was unsuccessful: %s", returnString)
	}

	//if success - take id from the response and update the state
	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminCreateMarking", err.Error())
		return fmt.Errorf("failed to parse response from AdminCreateMarking: %w", err)
	}
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	//CREATE - do not save state if id is not saved
	if httpResponseBody.ID == "" {
		tflog.Error(ctx, "ID was not populated in response, "+
			"so Terraform best practice is NOT to update state as resource likely was not properly created")
		resp.Diagnostics.AddError("ID returned as empty",
			"ID was not populated in response, "+
				"so Terraform best practice is NOT to update state as resource likely was not properly created")
		return fmt.Errorf("ID returned as empty: %s", httpResponseBody.ID)
	}

	//Set computed values
	plan.ID = types.StringValue(httpResponseBody.ID)
	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *markingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state markingResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	markingUUID, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("error parsing ID", err.Error())
	}

	err = r.ReadMarking(ctx, resp, &state, markingUUID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Marking", err.Error())
		return
	}

	if resp.Diagnostics.Warnings().Contains(providerError.ResourceNotFoundWarning(state.ID.ValueString(), "Marking")) {
		resp.State.RemoveResource(ctx)
		return
	}

	err = r.ReadMarkingMembers(ctx, &state, markingUUID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Marking members", err.Error())
	}

	err = r.ReadMarkingRoles(ctx, &state, markingUUID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Marking roles", err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingResource) ReadMarking(ctx context.Context, resp *resource.ReadResponse, state *markingResourceModel, markingUUID uuid.UUID) error {
	previewMode := constants.PreviewMode
	adminGetMarkingParams := v2.AdminGetMarkingParams{Preview: &previewMode}

	httpResp, err := r.client.AdminGetMarking(ctx, markingUUID, &adminGetMarkingParams)

	if err != nil {
		resp.Diagnostics.AddError("AdminGetMarking request failed", err.Error())
		return fmt.Errorf("AdminGetMarking request failed: %w", err)
	}
	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusNotFound {
			resp.Diagnostics.Append(providerError.ResourceNotFoundWarning(state.ID.ValueString(), "Marking"))
			return nil
		}
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminGetMarking response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminGetMarking response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminGetMarking was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminGetMarking was unsuccessful: %s", returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminGetMarking", err.Error())
		return fmt.Errorf("failed to parse response from AdminGetMarking: %w", err)
	}

	//if success - take id from the response and update the state
	var httpResponseBody responseBody
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	state.ID = types.StringValue(httpResponseBody.ID)
	state.CategoryID = types.StringValue(httpResponseBody.CategoryID)
	state.Name = types.StringValue(httpResponseBody.Name)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)
	return nil
}

func (r *markingResource) ReadMarkingMembers(ctx context.Context, state *markingResourceModel, markingUUID uuid.UUID) error {
	previewMode := constants.PreviewMode
	pageSize := constants.PageSize
	adminListMarkingMembersParams := v2.AdminListMarkingMembersParams{Preview: &previewMode, PageSize: &pageSize}
	httpResp, err := r.client.AdminListMarkingMembers(ctx, markingUUID, &adminListMarkingMembersParams)

	if err != nil {
		return fmt.Errorf("AdminListMarkingMembers request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminListMarkingMembers response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminListMarkingMembers: %w", err)
	}

	var httpMarkingMembersResponseBody markingMembersResponseBody
	if err := json.Unmarshal(bodyBytes, &httpMarkingMembersResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	markingMembersIds := make([]string, 0)
	for _, markingMember := range httpMarkingMembersResponseBody.Data {
		markingMembersIds = append(markingMembersIds, markingMember.PrincipalID)
	}

	if len(markingMembersIds) != 0 {
		state.MarkingMembers, _ = types.SetValueFrom(ctx, types.StringType, markingMembersIds)
	}
	return nil
}

func (r *markingResource) ReadMarkingRoles(ctx context.Context, state *markingResourceModel, markingUUID uuid.UUID) error {
	previewMode := constants.PreviewMode
	pageSize := constants.PageSize
	adminListMarkingRoleAssignmentsParams := v2.AdminListMarkingRoleAssignmentsParams{Preview: &previewMode, PageSize: &pageSize}
	httpResp, err := r.client.AdminListMarkingRoleAssignments(ctx, markingUUID, &adminListMarkingRoleAssignmentsParams)

	if err != nil {
		return fmt.Errorf("AdminListMarkingRoleAssignments request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminListMarkingRoleAssignments response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminListMarkingRoleAssignments: %w", err)
	}

	var httpMarkingRolesResponseBody markingRolesResponseBody
	if err := json.Unmarshal(bodyBytes, &httpMarkingRolesResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	roleAssignmentType := types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"principal_id": types.StringType,
			"role":         types.StringType,
		},
	}

	roleAssignments := make([]attr.Value, 0)
	for _, entry := range httpMarkingRolesResponseBody.Data {
		roleAssignment, _ := types.ObjectValue(
			roleAssignmentType.AttrTypes,
			map[string]attr.Value{
				"principal_id": types.StringValue(entry.PrincipalID),
				"role":         types.StringValue(entry.Role),
			},
		)
		roleAssignments = append(roleAssignments, roleAssignment)
	}

	if len(roleAssignments) != 0 {
		state.MarkingRoles, _ = types.SetValueFrom(ctx, roleAssignmentType, roleAssignments)
	}
	return nil
}

func (r *markingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan markingResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state markingResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateMarking(ctx, resp, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating marking. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	err = r.UpdateMarkingMembers(ctx, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating marking members. Please fix your plan if needed and re-apply", err.Error())
	}

	err = r.UpdateMarkingRoles(ctx, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating marking roles. Please fix your plan if needed and re-apply", err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingResource) UpdateMarking(ctx context.Context, resp *resource.UpdateResponse, plan *markingResourceModel, state *markingResourceModel) error {
	if plan.CategoryID != state.CategoryID {
		return fmt.Errorf("you may not change the category ID of a marking once it has been created. Please revert your plan to the existing category ID and re-apply")
	}
	previewMode := constants.PreviewMode
	adminReplaceMarkingParams := v2.AdminReplaceMarkingParams{Preview: &previewMode}
	description := plan.Description.ValueString()
	markingUUID, err := uuid.Parse(state.ID.ValueString())
	if err != nil {
		return fmt.Errorf("error parsing marking UUID: %w", err)
	}

	httpResp, err := r.client.AdminReplaceMarking(ctx, markingUUID, &adminReplaceMarkingParams, v2.AdminReplaceMarkingJSONRequestBody{
		Name:        plan.Name.ValueString(),
		Description: &description,
	})

	if err != nil {
		resp.Diagnostics.AddError("AdminReplaceMarking request failed", err.Error())
		return fmt.Errorf("AdminReplaceMarking request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminReplaceMarking response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminReplaceMarking response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminReplaceMarking was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminReplaceMarking was unsuccessful: %s", returnString)
	}

	//read body and then close
	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminReplaceMarking", err.Error())
		return fmt.Errorf("failed to parse response from AdminReplaceMarking: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	// Update the state with the new values

	state.Name = types.StringValue(httpResponseBody.Name)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)

	return nil
}

func (r *markingResource) UpdateMarkingMembers(ctx context.Context, plan, state *markingResourceModel) error {
	var oldMarkingMembers, newMarkingMembers []string

	//only initialize if not null, otherwise ElementsAs will throw error instead of just handling as empty slice
	if !state.MarkingMembers.IsNull() {
		diags := state.MarkingMembers.ElementsAs(ctx, &oldMarkingMembers, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert marking members to Go slice")
		}
	}

	if !plan.MarkingMembers.IsNull() {
		diags := plan.MarkingMembers.ElementsAs(ctx, &newMarkingMembers, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert planned marking members to Go slice")
		}
	}

	previewMode := constants.PreviewMode

	if !slices.Equal(oldMarkingMembers, newMarkingMembers) {
		membersToAdd, membersToRemove := helper.FindStringSliceDiff(oldMarkingMembers, newMarkingMembers)
		markingUUID, err := uuid.Parse(state.ID.ValueString())
		if err != nil {
			return fmt.Errorf("error parsing marking UUID: %w", err)
		}
		if len(membersToAdd) != 0 {
			params := v2.AdminAddMarkingMembersParams{Preview: &previewMode}
			httpResp, err := r.client.AdminAddMarkingMembers(ctx, markingUUID, &params, v2.AdminAddMarkingMembersJSONRequestBody{
				PrincipalIds: &membersToAdd,
			})
			if err != nil {
				return fmt.Errorf("AdminAddMarkingMembersParams request failed: %w", err)
			}
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminAddMarkingMembersParams response: %w", err)
				}
				return errors.New(returnString)
			}
		}
		if len(membersToRemove) != 0 {
			params := v2.AdminRemoveMarkingMembersParams{Preview: &previewMode}
			httpResp, err := r.client.AdminRemoveMarkingMembers(ctx, markingUUID, &params, v2.AdminRemoveMarkingMembersJSONRequestBody{
				PrincipalIds: &membersToRemove,
			})
			if err != nil {
				return fmt.Errorf("AdminRemoveMarkingMembers request failed: %w", err)
			}
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminAddGroupMembers response: %w", err)
				}
				return errors.New(returnString)
			}
		}
		//if there was a change (and no error thrown), update state to equal plan
		state.MarkingMembers = plan.MarkingMembers
	}
	return nil
}

func (r *markingResource) UpdateMarkingRoles(ctx context.Context, plan, state *markingResourceModel) error {
	var oldMarkingRoles, newMarkingRoles []RolesRequestBodyEntry

	if !state.MarkingRoles.IsNull() {
		diags := state.MarkingRoles.ElementsAs(ctx, &oldMarkingRoles, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert marking roles to Go slice")
		}
	}

	if !state.MarkingRoles.IsNull() {
		diags := plan.MarkingRoles.ElementsAs(ctx, &newMarkingRoles, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert marking roles to Go slice")
		}
	}

	previewMode := constants.PreviewMode
	if !slices.Equal(oldMarkingRoles, newMarkingRoles) {

		rolesToAdd, rolesToRemove := FindMarkingRolesDiff(oldMarkingRoles, newMarkingRoles)
		markingUUID, err := uuid.Parse(state.ID.ValueString())
		if err != nil {
			return fmt.Errorf("error parsing marking UUID: %w", err)
		}
		if len(rolesToAdd) != 0 {
			roleUpdates := make([]v2.AdminMarkingRoleUpdate, len(rolesToAdd))
			for i, role := range rolesToAdd {
				roleUpdates[i] = v2.AdminMarkingRoleUpdate{
					Role:        v2.AdminMarkingRole(role.Role),
					PrincipalID: role.PrincipalID,
				}
			}
			params := v2.AdminAddMarkingRoleAssignmentsParams{Preview: &previewMode}
			httpResp, err := r.client.AdminAddMarkingRoleAssignments(ctx, markingUUID, &params, v2.AdminAddMarkingRoleAssignmentsJSONRequestBody{
				RoleAssignments: &roleUpdates,
			})
			if err != nil {
				return fmt.Errorf("AdminAddMarkingRoleAssignments request failed: %w", err)
			}
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminAddGroupMembers response: %w", err)
				}
				return errors.New(returnString)
			}
		}
		if len(rolesToRemove) != 0 {
			roleUpdates := make([]v2.AdminMarkingRoleUpdate, len(rolesToRemove))
			for i, role := range rolesToRemove {
				roleUpdates[i] = v2.AdminMarkingRoleUpdate{
					Role:        v2.AdminMarkingRole(role.Role),
					PrincipalID: role.PrincipalID,
				}
			}
			params := v2.AdminRemoveMarkingRoleAssignmentsParams{Preview: &previewMode}
			httpResp, err := r.client.AdminRemoveMarkingRoleAssignments(ctx, markingUUID, &params, v2.AdminRemoveMarkingRoleAssignmentsJSONRequestBody{
				RoleAssignments: &roleUpdates,
			})
			if err != nil {
				return fmt.Errorf("AdminRemoveMarkingRoleAssignments request failed: %w", err)
			}
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminRemoveMarkingRoleAssignments response: %w", err)
				}
				return errors.New(returnString)
			}
		}
		//if there was a change (and no error thrown), update state to equal plan
		state.MarkingRoles = plan.MarkingRoles
	}
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *markingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	//return error here IMMEDIATELY, as Markings are not allowed to be deleted
	tflog.Error(ctx, "Markings cannot be deleted")
	resp.Diagnostics.AddError("Markings cannot be deleted",
		"Foundry does not support deleted markings!")
	return
}

// ImportState imports an existing marking into Terraform state.
func (r *markingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	markingID := req.ID

	if markingID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the marking RID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing marking with ID %s", markingID))

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), markingID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the ID
}

func FindMarkingRolesDiff(oldSlice, newSlice []RolesRequestBodyEntry) (added, removed []RolesRequestBodyEntry) {
	// Create maps for quick lookup
	oldMap := make(map[string]RolesRequestBodyEntry)
	newMap := make(map[string]RolesRequestBodyEntry)

	// Populate the maps with elements from the slices
	for _, item := range oldSlice {
		key := item.PrincipalID + "|" + item.Role
		oldMap[key] = item
	}
	for _, item := range newSlice {
		key := item.PrincipalID + "|" + item.Role
		newMap[key] = item
	}

	// Find added elements (in newSlice but not in oldSlice)
	for key, item := range newMap {
		if _, exists := oldMap[key]; !exists {
			added = append(added, item)
		}
	}

	// Find removed elements (in oldSlice but not in newSlice)
	for key, item := range oldMap {
		if _, exists := newMap[key]; !exists {
			removed = append(removed, item)
		}
	}

	return added, removed
}
