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

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/constants"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource                 = &enrollmentRoleAssignmentsResource{}
	_ resource.ResourceWithConfigure    = &enrollmentRoleAssignmentsResource{}
	_ resource.ResourceWithUpgradeState = &enrollmentRoleAssignmentsResource{}
)

// NewEnrollmentRoleAssignmentsResource is a helper function to simplify provider implementation.
func NewEnrollmentRoleAssignmentsResource() resource.Resource {
	return &enrollmentRoleAssignmentsResource{}
}

// enrollmentRoleAssignmentsResource is the resource implementation.
type enrollmentRoleAssignmentsResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider configured client to the resource.
func (r *enrollmentRoleAssignmentsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *enrollmentRoleAssignmentsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enrollment_role_assignments"
}

// Schema defines the schema for the resource.
func (r *enrollmentRoleAssignmentsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Enrollment's Role Assignments.",
		Version:     1,
		Attributes: map[string]schema.Attribute{
			"enrollment_rid": schema.StringAttribute{
				Description: "RID of the Enrollment.",
				Required:    true,
			},
			"enrollment_role_assignments": helper.RoleAssignmentMapSchema("Map of Role ID to set of Principal IDs for this Enrollment."),
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *enrollmentRoleAssignmentsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enrollmentRoleAssignmentsResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	if !plan.EnrollmentRoleAssignments.IsNull() {
		err := r.CreateEnrollmentRoleAssignments(ctx, resp, &plan)
		if err != nil {
			resp.Diagnostics.AddError("Error creating the Enrollment Role Assignments. Please fix your plan if needed and re-apply.", err.Error())
		}
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *enrollmentRoleAssignmentsResource) CreateEnrollmentRoleAssignments(ctx context.Context, resp *resource.CreateResponse, plan *enrollmentRoleAssignmentsResourceModel) error {
	newGenericEntries, err := helper.FlattenRoleAssignmentMap(ctx, plan.EnrollmentRoleAssignments)
	if err != nil {
		return fmt.Errorf("failed to convert planned role_assignments to Go slice: %w", err)
	}
	if !helper.MapHasRole(plan.EnrollmentRoleAssignments, constants.EnrollmentAdministratorRoleID) && !r.deletionsDisabled {
		return fmt.Errorf("the Enrollment must have at least one administrator")
	}

	oldRoleAssignments, err := r.ReadEnrollmentRoleAssignmentsOnCreation(ctx, plan)

	if err != nil {
		return fmt.Errorf("failed to read enrollment roles on creation: %w", err)
	}

	oldGenericEntries := enrollmentEntriesToGeneric(oldRoleAssignments)
	genericToAdd, genericToRemove := helper.FindRoleAssignmentsDiff(oldGenericEntries, newGenericEntries)
	rolesToAdd := genericToEnrollmentEntries(genericToAdd)
	rolesToRemove := genericToEnrollmentEntries(genericToRemove)

	if len(rolesToAdd) != 0 {
		err := r.AddEnrollmentRoleAssignments(ctx, rolesToAdd, plan.EnrollmentRID.ValueString())
		if err != nil {
			return err
		}
	}
	if len(rolesToRemove) != 0 && !r.deletionsDisabled {
		err := r.RemoveEnrollmentRoleAssignments(ctx, rolesToRemove, plan.EnrollmentRID.ValueString())
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
func (r *enrollmentRoleAssignmentsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state enrollmentRoleAssignmentsResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadEnrollmentRoleAssignments(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Enrollment role_assignments", err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *enrollmentRoleAssignmentsResource) ReadEnrollmentRoleAssignments(ctx context.Context, state *enrollmentRoleAssignmentsResourceModel) error {
	previewMode := constants.PreviewMode
	adminEnrollmentRoleAssignmentParams := v2.AdminListEnrollmentRoleAssignmentsParams{Preview: &previewMode}
	httpResp, err := r.client.AdminListEnrollmentRoleAssignments(ctx, state.EnrollmentRID.ValueString(), &adminEnrollmentRoleAssignmentParams)

	if err != nil {
		return fmt.Errorf("AdminListEnrollmentRoleAssignments request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminListEnrollmentRoleAssignments response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminListEnrollmentRoleAssignments: %w", err)
	}
	var httpEnrollmentRolesResponseBody enrollmentRolesResponseBody
	if err := json.Unmarshal(bodyBytes, &httpEnrollmentRolesResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	entries := make([]helper.RoleAssignmentEntry, len(httpEnrollmentRolesResponseBody.Data))
	for i, entry := range httpEnrollmentRolesResponseBody.Data {
		entries[i] = helper.RoleAssignmentEntry{RoleIdentifier: entry.RoleID, PrincipalID: entry.PrincipalID}
	}

	roleMap, err := helper.BuildRoleAssignmentMap(ctx, entries)
	if err != nil {
		return fmt.Errorf("failed to build role assignment map: %w", err)
	}
	state.EnrollmentRoleAssignments = roleMap
	return nil
}

func (r *enrollmentRoleAssignmentsResource) ReadEnrollmentRoleAssignmentsOnCreation(ctx context.Context, plan *enrollmentRoleAssignmentsResourceModel) ([]enrollmentRolesRequestBodyEntry, error) {
	previewMode := constants.PreviewMode
	adminEnrollmentRoleAssignmentParams := v2.AdminListEnrollmentRoleAssignmentsParams{Preview: &previewMode}
	httpResp, err := r.client.AdminListEnrollmentRoleAssignments(ctx, plan.EnrollmentRID.ValueString(), &adminEnrollmentRoleAssignmentParams)

	if err != nil {
		return nil, fmt.Errorf("AdminListEnrollmentRoleAssignments request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return nil, fmt.Errorf("failed to format error logging from AdminListEnrollmentRoleAssignments response: %w", err)
		}
		return nil, errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response from AdminListEnrollmentRoleAssignments: %w", err)
	}
	var httpEnrollmentRolesResponseBody enrollmentRolesResponseBody
	if err := json.Unmarshal(bodyBytes, &httpEnrollmentRolesResponseBody); err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	rolesToReturn := make([]enrollmentRolesRequestBodyEntry, 0)

	for _, entry := range httpEnrollmentRolesResponseBody.Data {
		roleAssignment := enrollmentRolesRequestBodyEntry{RoleID: entry.RoleID, PrincipalID: entry.PrincipalID}
		rolesToReturn = append(rolesToReturn, roleAssignment)
	}

	return rolesToReturn, nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *enrollmentRoleAssignmentsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan enrollmentRoleAssignmentsResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state enrollmentRoleAssignmentsResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateEnrollmentRoleAssignments(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Enrollment role_assignments. Please fix your plan if needed and re-apply", err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *enrollmentRoleAssignmentsResource) UpdateEnrollmentRoleAssignments(ctx context.Context, plan *enrollmentRoleAssignmentsResourceModel, state *enrollmentRoleAssignmentsResourceModel, resp *resource.UpdateResponse) error {
	oldGenericEntries, err := helper.FlattenRoleAssignmentMap(ctx, state.EnrollmentRoleAssignments)
	if err != nil {
		return fmt.Errorf("failed to convert enrollment roles to Go slice: %w", err)
	}

	newGenericEntries, err := helper.FlattenRoleAssignmentMap(ctx, plan.EnrollmentRoleAssignments)
	if err != nil {
		return fmt.Errorf("failed to convert enrollment roles to Go slice: %w", err)
	}

	if !helper.MapHasRole(plan.EnrollmentRoleAssignments, constants.EnrollmentAdministratorRoleID) && !r.deletionsDisabled {
		return fmt.Errorf("the Enrollment must have at least one administrator")
	}

	if !plan.EnrollmentRoleAssignments.Equal(state.EnrollmentRoleAssignments) {
		// Determine roles to add and remove
		genericToAdd, genericToRemove := helper.FindRoleAssignmentsDiff(oldGenericEntries, newGenericEntries)
		rolesToAdd := genericToEnrollmentEntries(genericToAdd)
		rolesToRemove := genericToEnrollmentEntries(genericToRemove)

		if len(rolesToAdd) != 0 {
			err := r.AddEnrollmentRoleAssignments(ctx, rolesToAdd, plan.EnrollmentRID.ValueString())
			if err != nil {
				return err
			}
		}
		if len(rolesToRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveEnrollmentRoleAssignments(ctx, rolesToRemove, plan.EnrollmentRID.ValueString())
			if err != nil {
				return err
			}
		} else if len(rolesToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found Role Assignments defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, Role Assignments removal operations will not be applied.")
		}
		state.EnrollmentRoleAssignments = plan.EnrollmentRoleAssignments
	}
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *enrollmentRoleAssignmentsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state enrollmentRoleAssignmentsResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning("Called Delete on an enrollment role_assignments resource.",
		fmt.Sprintf("The role_assignments resource for enrollment rid %s will be removed from state, but no role assignments will be removed remotely.", state.EnrollmentRID.ValueString()))
}

// ImportState imports existing enrollment role_assignments into Terraform state.
func (r *enrollmentRoleAssignmentsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is expected to be the enrollment RID
	enrollmentRID := req.ID

	// Validate the ID format
	if enrollmentRID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the enrollment RID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing enrollment role_assignments for enrollment with ID %s", enrollmentRID))

	// Set the Enrollment RID in state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enrollment_rid"), enrollmentRID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the RID
}

func (r *enrollmentRoleAssignmentsResource) AddEnrollmentRoleAssignments(ctx context.Context, rolesToAdd []enrollmentRolesRequestBodyEntry, rid string) error {
	previewMode := constants.PreviewMode

	roleUpdates := make([]v2.CoreRoleAssignmentUpdate, len(rolesToAdd))
	for i, role := range rolesToAdd {

		principalIDAsUUID, err := uuid.Parse(role.PrincipalID)

		if err != nil {
			return fmt.Errorf("invalid UUID format for principal ID %s: %w", role.PrincipalID, err)
		}

		roleUpdates[i] = v2.CoreRoleAssignmentUpdate{
			RoleID:      role.RoleID,
			PrincipalID: principalIDAsUUID,
		}
	}

	adminAddEnrollmentRoleAssignmentParams := v2.AdminAddEnrollmentRoleAssignmentsParams{Preview: &previewMode}
	httpResp, err := r.client.AdminAddEnrollmentRoleAssignments(ctx, rid, &adminAddEnrollmentRoleAssignmentParams, v2.AdminAddEnrollmentRoleAssignmentsJSONRequestBody{
		RoleAssignments: &roleUpdates,
	})

	if err != nil {
		return fmt.Errorf("AdminAddEnrollmentRoleAssignments request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminAddEnrollmentRoleAssignments response: %w", err)
		}
		return errors.New(returnString)
	}

	return nil
}

func (r *enrollmentRoleAssignmentsResource) RemoveEnrollmentRoleAssignments(ctx context.Context, rolesToRemove []enrollmentRolesRequestBodyEntry, rid string) error {
	previewMode := constants.PreviewMode

	roleUpdates := make([]v2.CoreRoleAssignmentUpdate, len(rolesToRemove))
	for i, role := range rolesToRemove {

		principalIDAsUUID, err := uuid.Parse(role.PrincipalID)

		if err != nil {
			return fmt.Errorf("invalid UUID format for principal ID %s: %w", role.PrincipalID, err)
		}

		roleUpdates[i] = v2.CoreRoleAssignmentUpdate{
			RoleID:      role.RoleID,
			PrincipalID: principalIDAsUUID,
		}
	}

	adminRemoveEnrollmentRoleAssignmentsParams := v2.AdminRemoveEnrollmentRoleAssignmentsParams{Preview: &previewMode}
	httpResp, err := r.client.AdminRemoveEnrollmentRoleAssignments(ctx, rid, &adminRemoveEnrollmentRoleAssignmentsParams, v2.AdminRemoveEnrollmentRoleAssignmentsJSONRequestBody{
		RoleAssignments: &roleUpdates,
	})

	if err != nil {
		return fmt.Errorf("AdminRemoveEnrollmentRoleAssignments request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminRemoveEnrollmentRoleAssignments response: %w", err)
		}
		return errors.New(returnString)
	}

	return nil
}

func enrollmentEntriesToGeneric(entries []enrollmentRolesRequestBodyEntry) []helper.RoleAssignmentEntry {
	result := make([]helper.RoleAssignmentEntry, len(entries))
	for i, e := range entries {
		result[i] = helper.RoleAssignmentEntry{RoleIdentifier: e.RoleID, PrincipalID: e.PrincipalID}
	}
	return result
}

func genericToEnrollmentEntries(entries []helper.RoleAssignmentEntry) []enrollmentRolesRequestBodyEntry {
	result := make([]enrollmentRolesRequestBodyEntry, len(entries))
	for i, e := range entries {
		result[i] = enrollmentRolesRequestBodyEntry{RoleID: e.RoleIdentifier, PrincipalID: e.PrincipalID}
	}
	return result
}
