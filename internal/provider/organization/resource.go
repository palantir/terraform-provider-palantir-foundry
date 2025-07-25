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

package organization

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
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource              = &organizationResource{}
	_ resource.ResourceWithConfigure = &organizationResource{}
)

func NewOrganizationResource() resource.Resource {
	return &organizationResource{}
}

// organizationResource is the resource implementation.
type organizationResource struct {
	client *v2.ClientWithResponses
}

// Configure adds the provider configured client to the resource.
func (r *organizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*v2.ClientWithResponses)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected v2.ClientWithResponses, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

// Metadata returns the resource type name.
func (r *organizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

// Schema defines the schema for the resource.
func (r *organizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry organization.",
		Attributes: map[string]schema.Attribute{
			"rid": schema.StringAttribute{
				Description: "Resource identifier of the organization.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the organization.",
				Required:    true,
			},
			"marking_id": schema.StringAttribute{
				Description: "Marking id of the organization",
				Computed:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the organization.",
				Optional:    true,
			},
			"host_name": schema.StringAttribute{
				Description: "The primary host name of the Organization. This should be used when constructing URLs for users of this Organization",
				Optional:    true,
			},
			"planned_organization_members": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "Planned list of the PrincipalIds of the members (users or groups) in this organization.",
				Required:    true,
			},
			"organization_members": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "Actual list of the PrincipalIds of the members (users or groups) in this organization., computed after successful addition/removal of members",
				Computed:    true,
			},
			"planned_organization_roles": schema.SetAttribute{
				Description: "Planned list of roles assigned to principals for this organization",
				Required:    true,
				ElementType: types.ObjectType{
					AttrTypes: map[string]attr.Type{
						"role_id":      types.StringType,
						"principal_id": types.StringType,
					},
				},
			},
			"organization_roles": schema.SetAttribute{
				Description: "Actual list of roles assigned to principals for this organization, computed after successful addition/removal of roles",
				Computed:    true,
				ElementType: types.ObjectType{
					AttrTypes: map[string]attr.Type{
						"role_id":      types.StringType,
						"principal_id": types.StringType,
					},
				},
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
// TODO: implement Create
func (r *organizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	tflog.Error(ctx, "Organizations cannot be created")
	resp.Diagnostics.AddError("Organizations cannot be created",
		"Foundry terraform provider currently does not support creating organizations")
	return
}

// Read refreshes the Terraform state with the latest data.
func (r *organizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state organizationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadOrganization(ctx, resp, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the organization resource",
			"Error reading the organization resource itself. Since this is the primary resource, nothing has been changed and we can safely return")
		return
	}

	err = r.ReadOrganizationMembers(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddWarning("Error reading the organization members",
			err.Error())
	}

	err = r.ReadOrganizationRoles(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddWarning("Error reading the organization roles",
			err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *organizationResource) ReadOrganization(ctx context.Context, resp *resource.ReadResponse, state *organizationResourceModel) error {
	previewMode := constants.PreviewMode
	adminGetOrganizationParams := v2.AdminGetOrganizationParams{Preview: &previewMode}

	httpResp, err := r.client.AdminGetOrganization(ctx, state.RID.ValueString(), &adminGetOrganizationParams)

	if err != nil {
		resp.Diagnostics.AddError("AdminGetOrganization request failed", err.Error())
		return fmt.Errorf("AdminGetOrganization request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusNotFound {
			resp.State.RemoveResource(ctx)
			return fmt.Errorf("organization not found, removing resource from Terraform state: %w", err)
		}
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminGetOrganization response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminGetOrganization response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminGetOrganization was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminGetOrganization was unsuccessful: %s", returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminGetOrganization", err.Error())
		return fmt.Errorf("failed to parse response from AdminGetOrganization: %w", err)
	}

	var httpResponseBody responseBody
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	state.RID = types.StringValue(httpResponseBody.RID)
	state.MarkingID = types.StringValue(httpResponseBody.MarkingID)
	state.Name = types.StringValue(httpResponseBody.Name)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)
	state.HostName = helper.HandleEmptyFieldString(httpResponseBody.Host)
	return nil
}

func (r *organizationResource) ReadOrganizationMembers(ctx context.Context, state *organizationResourceModel) error {
	markingUUID, err := uuid.Parse(state.MarkingID.ValueString())
	if err != nil {
		return fmt.Errorf("failed to parse marking UUID: %w", err)
	}

	previewMode := constants.PreviewMode
	pageSize := constants.PageSize
	adminListMarkingMembersParams := v2.AdminListMarkingMembersParams{Preview: &previewMode, PageSize: &pageSize}
	httpResp, err := r.client.AdminListMarkingMembers(ctx, markingUUID, &adminListMarkingMembersParams)

	if err != nil {
		return fmt.Errorf("AdminListMarkingMembers request failed: %w", err)
	}

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

	state.OrganizationMembers, _ = types.SetValueFrom(ctx, types.StringType, markingMembersIds)
	state.PlannedOrganizationMembers = state.OrganizationMembers
	return nil
}

func (r *organizationResource) ReadOrganizationRoles(ctx context.Context, state *organizationResourceModel) error {
	previewMode := constants.PreviewMode
	adminOrganizationRoleAssignmentParams := v2.AdminListOrganizationRoleAssignmentsParams{Preview: &previewMode}
	httpResp, err := r.client.AdminListOrganizationRoleAssignments(ctx, state.RID.ValueString(), &adminOrganizationRoleAssignmentParams)

	if err != nil {
		return fmt.Errorf("AdminListOrganizationRoleAssignments request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminListOrganizationRoleAssignments response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminListOrganizationRoleAssignments: %w", err)
	}
	var httpOrganizationRolesResponseBody organizationRolesResponseBody
	if err := json.Unmarshal(bodyBytes, &httpOrganizationRolesResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	roleAssignmentType := types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"principal_id": types.StringType,
			"role_id":      types.StringType,
		},
	}

	// Convert each entry to a map of attribute values
	roleAssignments := make([]attr.Value, 0)
	//var objects []attr.Value
	for _, entry := range httpOrganizationRolesResponseBody.Data {
		roleAssignment, _ := types.ObjectValue(
			roleAssignmentType.AttrTypes,
			map[string]attr.Value{
				"principal_id": types.StringValue(entry.PrincipalID),
				"role_id":      types.StringValue(entry.RoleID),
			},
		)
		roleAssignments = append(roleAssignments, roleAssignment)
	}

	// Create the set from the list of objects
	state.OrganizationRoles, _ = types.SetValueFrom(ctx, roleAssignmentType, roleAssignments)
	state.PlannedOrganizationRoles = state.OrganizationRoles
	return nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *organizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan organizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state organizationResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateOrganization(ctx, resp, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the organization resource",
			"Error updating the organization resource itself. Since this is the primary resource, nothing has been changed and we can safely return")
		return
	}

	err = r.UpdateOrganizationMembers(ctx, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddWarning("Error updating the organization members",
			err.Error())
	}

	err = r.UpdateOrganizationRoles(ctx, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddWarning("Error updating the organization roles",
			err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *organizationResource) UpdateOrganization(ctx context.Context, resp *resource.UpdateResponse, plan *organizationResourceModel, state *organizationResourceModel) error {
	previewMode := constants.PreviewMode

	adminReplaceOrganizationParams := v2.AdminReplaceOrganizationParams{Preview: &previewMode}
	description := plan.Description.ValueString()
	host := plan.HostName.ValueString()

	httpResp, err := r.client.AdminReplaceOrganization(ctx, state.RID.ValueString(), &adminReplaceOrganizationParams, v2.AdminReplaceOrganizationJSONRequestBody{
		Name:        plan.Name.ValueString(),
		Description: &description,
		Host:        &host,
	})

	if err != nil {
		resp.Diagnostics.AddError("AdminReplaceOrganization request failed", err.Error())
		return fmt.Errorf("AdminReplaceOrganization request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminReplaceOrganization response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminReplaceOrganization response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminReplaceOrganization was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminReplaceOrganization was unsuccessful: %s", returnString)
	}

	//read body and then close
	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminReplaceOrganization", err.Error())
		return fmt.Errorf("failed to parse response from AdminReplaceOrganization: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	// Update the state with the new values

	state.Name = types.StringValue(httpResponseBody.Name)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)
	state.HostName = helper.HandleEmptyFieldString(httpResponseBody.Host)

	return nil
}

func (r *organizationResource) UpdateOrganizationMembers(ctx context.Context, plan *organizationResourceModel, state *organizationResourceModel) error {
	var oldMarkingMembers []string
	var newMarkingMembers []string

	diags := state.OrganizationMembers.ElementsAs(ctx, &oldMarkingMembers, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert organization members to Go slice")
	}

	diags = plan.PlannedOrganizationMembers.ElementsAs(ctx, &newMarkingMembers, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert planned organization members to Go slice")
	}

	previewMode := constants.PreviewMode

	if !slices.Equal(oldMarkingMembers, newMarkingMembers) {
		membersToAdd, membersToRemove := helper.FindStringSliceDiff(oldMarkingMembers, newMarkingMembers)
		markingUUID, err := uuid.Parse(state.MarkingID.ValueString())
		if err != nil {
			return fmt.Errorf("error parsing marking UUID: %w", err)
		}
		if len(membersToAdd) != 0 {
			adminAddMarkingRoleAssignmentsParams := v2.AdminAddMarkingMembersParams{Preview: &previewMode}
			httpResp, err := r.client.AdminAddMarkingMembers(ctx, markingUUID, &adminAddMarkingRoleAssignmentsParams, v2.AdminAddMarkingMembersJSONRequestBody{
				PrincipalIds: &membersToAdd,
			})

			if err != nil {
				return fmt.Errorf("AdminAddMarkingMembers request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminAddMarkingMembers response: %w", err)
				}
				if plan.OrganizationMembers.IsUnknown() {
					plan.OrganizationMembers = state.OrganizationMembers
				}
				state.PlannedOrganizationMembers = plan.OrganizationMembers
				return errors.New(returnString)
			}
			plan.OrganizationMembers = plan.PlannedOrganizationMembers
		}
		if len(membersToRemove) != 0 {
			adminRemoveMarkingRoleAssignmentsParams := v2.AdminRemoveMarkingMembersParams{Preview: &previewMode}
			httpResp, err := r.client.AdminRemoveMarkingMembers(ctx, markingUUID, &adminRemoveMarkingRoleAssignmentsParams, v2.AdminRemoveMarkingMembersJSONRequestBody{
				PrincipalIds: &membersToRemove,
			})

			if err != nil {
				return fmt.Errorf("AdminRemoveMarkingMembers request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminRemoveMarkingMembers response: %w", err)
				}
				if plan.OrganizationMembers.IsUnknown() {
					plan.OrganizationMembers = state.OrganizationMembers
				}
				state.PlannedOrganizationMembers = plan.OrganizationMembers
				return errors.New(returnString)
			}
			plan.OrganizationMembers = plan.PlannedOrganizationMembers
		}
		state.OrganizationMembers = plan.OrganizationMembers
	}
	state.PlannedOrganizationMembers = plan.PlannedOrganizationMembers
	return nil
}

func (r *organizationResource) UpdateOrganizationRoles(ctx context.Context, plan *organizationResourceModel, state *organizationResourceModel) error {

	var oldOrganizationRoles []organizationRolesRequestBodyEntry
	diags := state.OrganizationRoles.ElementsAs(ctx, &oldOrganizationRoles, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert org roles to Go slice")
	}

	var newOrganizationRoles []organizationRolesRequestBodyEntry
	diags = plan.PlannedOrganizationRoles.ElementsAs(ctx, &newOrganizationRoles, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert org roles to Go slice")
	}

	previewMode := constants.PreviewMode

	if !slices.Equal(oldOrganizationRoles, newOrganizationRoles) {
		// Determine members to add and remove
		rolesToAdd, rolesToRemove := findOrganizationRolesDiff(oldOrganizationRoles, newOrganizationRoles)
		if len(rolesToAdd) != 0 {

			roleUpdates := make([]v2.CoreRoleAssignmentUpdate, len(rolesToAdd))
			for i, role := range rolesToAdd {
				roleUpdates[i] = v2.CoreRoleAssignmentUpdate{
					RoleID:      role.RoleID,
					PrincipalID: role.PrincipalID,
				}
			}

			adminAddOrganizationRoleAssignmentParams := v2.AdminAddOrganizationRoleAssignmentsParams{Preview: &previewMode}
			httpResp, err := r.client.AdminAddOrganizationRoleAssignments(ctx, state.RID.ValueString(), &adminAddOrganizationRoleAssignmentParams, v2.AdminAddOrganizationRoleAssignmentsJSONRequestBody{
				RoleAssignments: &roleUpdates,
			})

			if err != nil {
				return fmt.Errorf("AdminAddOrganizationRoleAssignments request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminAddOrganizationRoleAssignments response: %w", err)
				}
				if plan.OrganizationRoles.IsUnknown() {
					plan.OrganizationRoles = state.OrganizationRoles
				}
				state.PlannedOrganizationRoles = plan.PlannedOrganizationRoles
				return errors.New(returnString)
			}
			plan.OrganizationRoles = plan.PlannedOrganizationRoles
		}
		if len(rolesToRemove) != 0 {
			roleUpdates := make([]v2.CoreRoleAssignmentUpdate, len(rolesToRemove))
			for i, role := range rolesToRemove {
				roleUpdates[i] = v2.CoreRoleAssignmentUpdate{
					RoleID:      role.RoleID,
					PrincipalID: role.PrincipalID,
				}
			}

			adminRemoveOrganizationRoleAssignmentsParams := v2.AdminRemoveOrganizationRoleAssignmentsParams{Preview: &previewMode}
			httpResp, err := r.client.AdminRemoveOrganizationRoleAssignments(ctx, state.RID.ValueString(), &adminRemoveOrganizationRoleAssignmentsParams, v2.AdminRemoveOrganizationRoleAssignmentsJSONRequestBody{
				RoleAssignments: &roleUpdates,
			})

			if err != nil {
				return fmt.Errorf("AdminRemoveOrganizationRoleAssignments request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminRemoveOrganizationRoleAssignments response: %w", err)
				}
				if plan.OrganizationRoles.IsUnknown() {
					plan.OrganizationRoles = state.OrganizationRoles
				}
				state.PlannedOrganizationRoles = plan.PlannedOrganizationRoles
				return errors.New(returnString)
			}
			plan.OrganizationRoles = plan.PlannedOrganizationRoles
		}
		state.OrganizationRoles = plan.OrganizationRoles
	}
	state.PlannedOrganizationRoles = plan.PlannedOrganizationRoles
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *organizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	//return error here IMMEDIATELY, as Markings are not allowed to be deleted
	tflog.Error(ctx, "Organizations cannot be deleted")
	resp.Diagnostics.AddError("Organizations cannot be deleted",
		"Foundry does not support deleted Organizations!")
	return
}

// ImportState imports an existing organization into Terraform state.
func (r *organizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is expected to be the organization RID
	organizationRID := req.ID

	// Validate the ID format (optional, can add your own validation logic)
	if organizationRID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the organization RID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing organization with RID %s", organizationRID))

	// Set the organization RID in state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rid"), organizationRID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the RID
}

func findOrganizationRolesDiff(oldSlice, newSlice []organizationRolesRequestBodyEntry) (added, removed []organizationRolesRequestBodyEntry) {
	// Create maps for quick lookup
	oldMap := make(map[string]organizationRolesRequestBodyEntry)
	newMap := make(map[string]organizationRolesRequestBodyEntry)

	// Populate the maps with elements from the slices
	for _, item := range oldSlice {
		key := item.PrincipalID + "|" + item.RoleID
		oldMap[key] = item
	}
	for _, item := range newSlice {
		key := item.PrincipalID + "|" + item.RoleID
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
