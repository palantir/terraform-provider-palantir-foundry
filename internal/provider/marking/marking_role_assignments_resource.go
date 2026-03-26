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

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/constants"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource                 = &markingRoleAssignmentsResource{}
	_ resource.ResourceWithConfigure    = &markingRoleAssignmentsResource{}
	_ resource.ResourceWithUpgradeState = &markingRoleAssignmentsResource{}
)

// NewMarkingRoleAssignmentsResource is a helper function to simplify provider implementation.
func NewMarkingRoleAssignmentsResource() resource.Resource {
	return &markingRoleAssignmentsResource{}
}

// markingRoleAssignmentsResource is the resource implementation.
type markingRoleAssignmentsResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider configured client to the resource.
func (r *markingRoleAssignmentsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*shared.FoundryProviderData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected v2.ClientWithResponses, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = providerData.Client
	r.deletionsDisabled = providerData.Flags.DeletionsDisabled
}

// Metadata returns the resource type name.
func (r *markingRoleAssignmentsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_marking_role_assignments"
}

// Schema defines the schema for the resource.
func (r *markingRoleAssignmentsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	markingRoleAssignmentsSchema := helper.RoleAssignmentMapSchema("Map of Role to set of Principal IDs for this Marking. " +
		"The following Roles can be assigned to a Marking: \n - ADMINISTER: The user can add and remove members from the Marking, update Marking Role Assignments, and change Marking metadata.\n - DECLASSIFY: The user can remove the Marking from resources in the platform and stop the propagation of the Marking during a transform.\n - USE: The user can apply the Marking to resources in the platform.")
	markingRoleAssignmentsSchema.Required = false
	markingRoleAssignmentsSchema.Optional = true
	markingRoleAssignmentsSchema.Validators = []validator.Map{
		mapvalidator.KeysAre(stringvalidator.OneOf("ADMINISTER", "DECLASSIFY", "USE")),
	}

	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Marking's Role Assignments.",
		Version:     1,
		Attributes: map[string]schema.Attribute{
			"marking_id": schema.StringAttribute{
				Description: "ID of the Marking.",
				Required:    true,
			},
			"marking_role_assignments": markingRoleAssignmentsSchema,
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *markingRoleAssignmentsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan markingRoleAssignmentsResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	if !plan.MarkingRoleAssignments.IsNull() {
		err := r.CreateMarkingRoleAssignments(ctx, resp, &plan)
		if err != nil {
			resp.Diagnostics.AddError("Error creating the Marking Role Assignments. Please fix your plan if needed and re-apply.", err.Error())
		}
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingRoleAssignmentsResource) CreateMarkingRoleAssignments(ctx context.Context, resp *resource.CreateResponse, plan *markingRoleAssignmentsResourceModel) error {
	newGenericEntries, err := helper.FlattenRoleAssignmentMap(ctx, plan.MarkingRoleAssignments)
	if err != nil {
		return fmt.Errorf("failed to convert planned role_assignments to Go slice: %w", err)
	}

	oldRoleAssignments, err := r.ReadMarkingRoleAssignmentsOnCreation(ctx, plan)

	if err != nil {
		return fmt.Errorf("failed to read marking orgs on creation: %w", err)
	}

	oldGenericEntries := markingEntriesToGeneric(oldRoleAssignments)
	genericToAdd, genericToRemove := helper.FindRoleAssignmentsDiff(oldGenericEntries, newGenericEntries)
	rolesToAdd := genericToMarkingEntries(genericToAdd)
	rolesToRemove := genericToMarkingEntries(genericToRemove)

	if len(rolesToAdd) != 0 {
		err := r.AddMarkingRoleAssignments(ctx, rolesToAdd, plan.MarkingID.ValueString())
		if err != nil {
			return err
		}
	}
	if len(rolesToRemove) != 0 && !r.deletionsDisabled {
		err := r.RemoveMarkingRoleAssignments(ctx, rolesToRemove, plan.MarkingID.ValueString())
		if err != nil {
			return err
		}
	} else if len(rolesToRemove) != 0 {
		resp.Diagnostics.AddWarning("Found Role Assignments defined in the state that are not in the plan.",
			"Since `deletions_disabled` is set to true, Role Assignments removal operations will not be applied.")
	}

	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *markingRoleAssignmentsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state markingRoleAssignmentsResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadMarkingRoleAssignments(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Marking role_assignments", err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingRoleAssignmentsResource) ReadMarkingRoleAssignments(ctx context.Context, state *markingRoleAssignmentsResourceModel) error {
	pageSize := constants.PageSize
	adminListMarkingRoleAssignmentsParams := v2.AdminListMarkingRoleAssignmentsParams{PageSize: &pageSize}
	httpResp, err := r.client.AdminListMarkingRoleAssignments(ctx, state.MarkingID.ValueString(), &adminListMarkingRoleAssignmentsParams)

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

	entries := make([]helper.RoleAssignmentEntry, len(httpMarkingRolesResponseBody.Data))
	for i, entry := range httpMarkingRolesResponseBody.Data {
		entries[i] = helper.RoleAssignmentEntry{RoleIdentifier: entry.Role, PrincipalID: entry.PrincipalID}
	}

	roleMap, err := helper.BuildRoleAssignmentMap(ctx, entries)
	if err != nil {
		return fmt.Errorf("failed to build role assignment map: %w", err)
	}
	state.MarkingRoleAssignments = roleMap
	return nil
}

func (r *markingRoleAssignmentsResource) ReadMarkingRoleAssignmentsOnCreation(ctx context.Context, plan *markingRoleAssignmentsResourceModel) ([]markingRolesRequestBodyEntry, error) {
	pageSize := constants.PageSize
	adminListMarkingRoleAssignmentsParams := v2.AdminListMarkingRoleAssignmentsParams{PageSize: &pageSize}
	httpResp, err := r.client.AdminListMarkingRoleAssignments(ctx, plan.MarkingID.ValueString(), &adminListMarkingRoleAssignmentsParams)

	if err != nil {
		return nil, fmt.Errorf("AdminListMarkingRoleAssignments request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return nil, fmt.Errorf("failed to format error logging from AdminListMarkingRoleAssignments response: %w", err)
		}
		return nil, errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response from AdminListMarkingRoleAssignments: %w", err)
	}
	var httpMarkingRolesResponseBody markingRolesResponseBody
	if err := json.Unmarshal(bodyBytes, &httpMarkingRolesResponseBody); err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	rolesToReturn := make([]markingRolesRequestBodyEntry, 0)

	for _, entry := range httpMarkingRolesResponseBody.Data {
		roleAssignment := markingRolesRequestBodyEntry{Role: entry.Role, PrincipalID: entry.PrincipalID}
		rolesToReturn = append(rolesToReturn, roleAssignment)
	}

	return rolesToReturn, nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *markingRoleAssignmentsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan markingRoleAssignmentsResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state markingRoleAssignmentsResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateMarkingRoleAssignments(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Marking role_assignments. Please fix your plan if needed and re-apply", err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *markingRoleAssignmentsResource) UpdateMarkingRoleAssignments(ctx context.Context, plan *markingRoleAssignmentsResourceModel, state *markingRoleAssignmentsResourceModel, resp *resource.UpdateResponse) error {
	oldGenericEntries, err := helper.FlattenRoleAssignmentMap(ctx, state.MarkingRoleAssignments)
	if err != nil {
		return fmt.Errorf("failed to convert marking roles to Go slice: %w", err)
	}

	newGenericEntries, err := helper.FlattenRoleAssignmentMap(ctx, plan.MarkingRoleAssignments)
	if err != nil {
		return fmt.Errorf("failed to convert marking roles to Go slice: %w", err)
	}

	if !plan.MarkingRoleAssignments.Equal(state.MarkingRoleAssignments) {
		// Determine roles to add and remove
		genericToAdd, genericToRemove := helper.FindRoleAssignmentsDiff(oldGenericEntries, newGenericEntries)
		rolesToAdd := genericToMarkingEntries(genericToAdd)
		rolesToRemove := genericToMarkingEntries(genericToRemove)

		if len(rolesToAdd) != 0 {
			err := r.AddMarkingRoleAssignments(ctx, rolesToAdd, plan.MarkingID.ValueString())
			if err != nil {
				return err
			}
		}
		if len(rolesToRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveMarkingRoleAssignments(ctx, rolesToRemove, plan.MarkingID.ValueString())
			if err != nil {
				return err
			}
		} else if len(rolesToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found Role Assignments defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, Role Assignments removal operations will not be applied.")
		}
		state.MarkingRoleAssignments = plan.MarkingRoleAssignments
	}
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *markingRoleAssignmentsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state markingRoleAssignmentsResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning("Called Delete on a marking role_assignments resource.",
		fmt.Sprintf("The role_assignments resource for marking rid %s will be removed from state, but no role assignments will be removed remotely.", state.MarkingID.ValueString()))

}

// ImportState imports existing marking role_assignments into Terraform state.
func (r *markingRoleAssignmentsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is expected to be the marking ID
	markingID := req.ID

	// Validate the ID format
	if markingID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the marking ID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing marking role_assignments for marking with ID %s", markingID))

	// Set the Marking ID in state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("marking_id"), markingID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the ID
}

func (r *markingRoleAssignmentsResource) AddMarkingRoleAssignments(ctx context.Context, rolesToAdd []markingRolesRequestBodyEntry, id string) error {
	roleUpdates := make([]v2.AdminMarkingRoleUpdate, len(rolesToAdd))
	for i, role := range rolesToAdd {
		principalIDAsUUID, err := uuid.Parse(role.PrincipalID)

		if err != nil {
			return fmt.Errorf("invalid UUID format for principal ID %s: %w", role.PrincipalID, err)
		}
		roleUpdates[i] = v2.AdminMarkingRoleUpdate{
			Role:        v2.AdminMarkingRole(role.Role),
			PrincipalID: principalIDAsUUID,
		}
	}
	httpResp, err := r.client.AdminAddMarkingRoleAssignments(ctx, id, v2.AdminAddMarkingRoleAssignmentsJSONRequestBody{
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

	return nil
}

func (r *markingRoleAssignmentsResource) RemoveMarkingRoleAssignments(ctx context.Context, rolesToRemove []markingRolesRequestBodyEntry, id string) error {
	roleUpdates := make([]v2.AdminMarkingRoleUpdate, len(rolesToRemove))
	for i, role := range rolesToRemove {
		principalIDAsUUID, err := uuid.Parse(role.PrincipalID)

		if err != nil {
			return fmt.Errorf("invalid UUID format for principal ID %s: %w", role.PrincipalID, err)
		}

		roleUpdates[i] = v2.AdminMarkingRoleUpdate{
			Role:        v2.AdminMarkingRole(role.Role),
			PrincipalID: principalIDAsUUID,
		}
	}
	httpResp, err := r.client.AdminRemoveMarkingRoleAssignments(ctx, id, v2.AdminRemoveMarkingRoleAssignmentsJSONRequestBody{
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

	return nil
}

func markingEntriesToGeneric(entries []markingRolesRequestBodyEntry) []helper.RoleAssignmentEntry {
	result := make([]helper.RoleAssignmentEntry, len(entries))
	for i, e := range entries {
		result[i] = helper.RoleAssignmentEntry{RoleIdentifier: e.Role, PrincipalID: e.PrincipalID}
	}
	return result
}

func genericToMarkingEntries(entries []helper.RoleAssignmentEntry) []markingRolesRequestBodyEntry {
	result := make([]markingRolesRequestBodyEntry, len(entries))
	for i, e := range entries {
		result[i] = markingRolesRequestBodyEntry{Role: e.RoleIdentifier, PrincipalID: e.PrincipalID}
	}
	return result
}
