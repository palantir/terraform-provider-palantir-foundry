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
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
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
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider data to the resource.
func (r *organizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *organizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

// Schema defines the schema for the resource.
func (r *organizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Organization.",
		Attributes: map[string]schema.Attribute{
			"rid": schema.StringAttribute{
				Description: "RID of the Organization.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the Organization.",
				Required:    true,
			},
			"marking_id": schema.StringAttribute{
				Description: "Marking ID of the Organization.",
				Computed:    true,
			},
			"enrollment_rid": schema.StringAttribute{
				Description: "The RID of the Enrollment this Organization belongs to. This field required if the resource is created within Terraform, but not necessarily if created outside of Terraform and imported.",
				Optional:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the Organization.",
				Optional:    true,
			},
			"host_name": schema.StringAttribute{
				Description: "The primary host name of the Organization. This should be used when constructing URLs for users of this Organization.",
				Optional:    true,
			},
			"organization_members": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "List of the IDs of the members that belong to this Organization.",
				Optional:    true,
			},
			"organization_roles": schema.SetAttribute{
				Description: "List of role assignments for this Organization.",
				Required:    true,
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
func (r *organizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan organizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	// Here, we are parsing out the provided organization roles from the plan. The administrator roles are passed into CreateOrganization so that the organization can be initialized with the administrators. The non-administrator roles are passed into CreateOrganizationNonAdministratorRoles so that they can be added after the organization is created.
	var allRoles []organizationRolesRequestBodyEntry
	diags = plan.OrganizationRoles.ElementsAs(ctx, &allRoles, false)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	var adminPrincipalIDs []v2.CorePrincipalID
	var nonAdminPrincipalIds []v2.CoreRoleAssignmentUpdate
	for _, role := range allRoles {
		principalID := role.PrincipalID
		if role.RoleID == constants.OrganizationAdministratorRoleID {
			adminPrincipalIDs = append(adminPrincipalIDs, principalID)
		} else {
			nonAdminPrincipalIds = append(nonAdminPrincipalIds, v2.CoreRoleAssignmentUpdate{
				RoleID:      role.RoleID,
				PrincipalID: role.PrincipalID,
			})
		}
	}

	if len(adminPrincipalIDs) == 0 {
		resp.Diagnostics.AddError("Error creating the Organization. Please fix your plan if needed and re-apply", "the Organization must have at least one administrator")
		return
	}

	err := r.CreateOrganization(ctx, resp, &plan, adminPrincipalIDs)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the Organization. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	err = r.CreateOrganizationMembers(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the Organization members. Please fix your plan if needed and re-apply.", err.Error())
	}

	if len(nonAdminPrincipalIds) > 0 {
		err = r.CreateOrganizationNonAdministratorRoles(ctx, resp, &plan, nonAdminPrincipalIds)
		if err != nil {
			resp.Diagnostics.AddError("Error creating the Organization roles. Please fix your plan if needed and re-apply.", err.Error())
		}
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *organizationResource) CreateOrganization(ctx context.Context, resp *resource.CreateResponse, plan *organizationResourceModel, adminPrincipalIDs []v2.CorePrincipalID) error {

	previewMode := constants.PreviewMode
	adminCreateOrganizationParams := v2.AdminCreateOrganizationParams{Preview: &previewMode}
	description := plan.Description.ValueString()
	host := plan.HostName.ValueString()

	httpResp, err := r.client.AdminCreateOrganization(ctx,
		&adminCreateOrganizationParams,
		v2.AdminCreateOrganizationJSONRequestBody{
			Name:           plan.Name.ValueString(),
			Description:    &description,
			Administrators: &adminPrincipalIDs,
			EnrollmentRid:  plan.EnrollmentRID.ValueString(),
			Host:           &host,
		})

	if err != nil {
		resp.Diagnostics.AddError("AdminCreateOrganization request failed", err.Error())
		return fmt.Errorf("AdminCreateOrganization request failed: %w", err)
	}
	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminCreateOrganization response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminCreateOrganization response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminCreateOrganization was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminCreateOrganization was unsuccessful: %s", returnString)
	}

	//if success - take id from the response and update the state
	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminCreateOrganization", err.Error())
		return fmt.Errorf("failed to parse response from AdminCreateOrganization: %w", err)
	}
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	//CREATE - do not save state if id is not saved
	if httpResponseBody.RID == "" {
		tflog.Error(ctx, "RID was not populated in response, "+
			"so Terraform best practice is NOT to update state as resource likely was not properly created")
		resp.Diagnostics.AddError("ID returned as empty",
			"ID was not populated in response, "+
				"so Terraform best practice is NOT to update state as resource likely was not properly created")
		return fmt.Errorf("ID returned as empty: %s", httpResponseBody.RID)
	}

	//set computed values
	plan.RID = types.StringValue(httpResponseBody.RID)
	plan.MarkingID = types.StringValue(httpResponseBody.MarkingID)
	return nil
}

func (r *organizationResource) CreateOrganizationMembers(ctx context.Context, resp *resource.CreateResponse, plan *organizationResourceModel) error {
	var plannedOrganizationMembers []v2.CorePrincipalID
	diags := plan.OrganizationMembers.ElementsAs(ctx, &plannedOrganizationMembers, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert planned group members to Go slice")
	}

	markingUUID, err := uuid.Parse(plan.MarkingID.ValueString())
	if err != nil {
		return fmt.Errorf("failed to parse marking UUID: %w", err)
	}
	previewMode := constants.PreviewMode

	adminAddMarkingRoleAssignmentsParams := v2.AdminAddMarkingMembersParams{Preview: &previewMode}
	httpResp, err := r.client.AdminAddMarkingMembers(ctx, markingUUID, &adminAddMarkingRoleAssignmentsParams, v2.AdminAddMarkingMembersJSONRequestBody{
		PrincipalIds: &plannedOrganizationMembers,
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
		plan.OrganizationMembers, diags = types.SetValueFrom(ctx, types.StringType, make([]string, 0))
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return fmt.Errorf("failed to initialize organization members in plan")
		}
		return errors.New(returnString)
	}
	return nil
}

func (r *organizationResource) CreateOrganizationNonAdministratorRoles(ctx context.Context, resp *resource.CreateResponse, plan *organizationResourceModel, nonAdminRoleAssignments []v2.CoreRoleAssignmentUpdate) error {
	previewMode := constants.PreviewMode

	adminAddOrganizationRoleAssignmentParams := v2.AdminAddOrganizationRoleAssignmentsParams{Preview: &previewMode}
	httpResp, err := r.client.AdminAddOrganizationRoleAssignments(ctx, plan.RID.ValueString(), &adminAddOrganizationRoleAssignmentParams, v2.AdminAddOrganizationRoleAssignmentsJSONRequestBody{
		RoleAssignments: &nonAdminRoleAssignments,
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
		return errors.New(returnString)
	}
	return nil
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
		resp.Diagnostics.AddError("Error reading the Organization", err.Error())
		return
	}

	if resp.Diagnostics.Warnings().Contains(providerError.ResourceNotFoundWarning(state.RID.ValueString(), "Organization")) {
		resp.State.RemoveResource(ctx)
		return
	}

	err = r.ReadOrganizationMembers(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Organization members", err.Error())
	}

	err = r.ReadOrganizationRoles(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Organization roles", err.Error())
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
			resp.Diagnostics.Append(providerError.ResourceNotFoundWarning(state.RID.ValueString(), "Organization"))
			return nil
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

	if len(markingMembersIds) != 0 {
		state.OrganizationMembers, _ = types.SetValueFrom(ctx, types.StringType, markingMembersIds)
	}
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

	if len(roleAssignments) != 0 {
		state.OrganizationRoles, _ = types.SetValueFrom(ctx, roleAssignmentType, roleAssignments)
	}
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
		resp.Diagnostics.AddError("Error updating the Organization. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	err = r.UpdateOrganizationMembers(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Organization members. Please fix your plan if needed and re-apply", err.Error())
	}

	err = r.UpdateOrganizationRoles(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Organization roles. Please fix your plan if needed and re-apply",
			err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *organizationResource) UpdateOrganization(ctx context.Context, resp *resource.UpdateResponse, plan *organizationResourceModel, state *organizationResourceModel) error {
	if state.EnrollmentRID != plan.EnrollmentRID {
		return fmt.Errorf("you may not change the Enrollment RID of an Organization once it has been created. Please revert your plan to the existing Enrollment RID and re-apply")
	}
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

func (r *organizationResource) UpdateOrganizationMembers(ctx context.Context, plan *organizationResourceModel, state *organizationResourceModel, resp *resource.UpdateResponse) error {
	var oldMarkingMembers []string
	var newMarkingMembers []string

	if !state.OrganizationMembers.IsNull() {
		diags := state.OrganizationMembers.ElementsAs(ctx, &oldMarkingMembers, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert organization members to Go slice")
		}
	}

	if !plan.OrganizationMembers.IsNull() {
		diags := plan.OrganizationMembers.ElementsAs(ctx, &newMarkingMembers, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert planned organization members to Go slice")
		}
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
				return errors.New(returnString)
			}
		}
		if len(membersToRemove) != 0 && !r.deletionsDisabled {
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
				return errors.New(returnString)
			}
		} else if len(membersToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found organization members in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, member-removal operations will not be applied.")
		}
		state.OrganizationMembers = plan.OrganizationMembers
	}
	return nil
}

func (r *organizationResource) UpdateOrganizationRoles(ctx context.Context, plan *organizationResourceModel, state *organizationResourceModel, resp *resource.UpdateResponse) error {

	var oldOrganizationRoles []organizationRolesRequestBodyEntry
	var newOrganizationRoles []organizationRolesRequestBodyEntry

	diags := state.OrganizationRoles.ElementsAs(ctx, &oldOrganizationRoles, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert org roles to Go slice")
	}

	diags = plan.OrganizationRoles.ElementsAs(ctx, &newOrganizationRoles, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert org roles to Go slice")
	}

	hasAdmin := false
	for _, role := range newOrganizationRoles {
		if role.RoleID == constants.OrganizationAdministratorRoleID {
			hasAdmin = true
			break
		}
	}
	if !hasAdmin && !r.deletionsDisabled {
		return fmt.Errorf("the Organization must have at least one administrator")
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
				return errors.New(returnString)
			}
		}
		if len(rolesToRemove) != 0 && !r.deletionsDisabled {
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
				return errors.New(returnString)
			}
		} else if len(rolesToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found roles defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, role-removal operations will not be applied.")
		}
		state.OrganizationRoles = plan.OrganizationRoles
	}
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *organizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.deletionsDisabled {
		tflog.Warn(ctx, "Organizations cannot be deleted")
		resp.Diagnostics.AddWarning("Organizations cannot be deleted",
			"Foundry does not support deleted Organizations. Since deletions_disabled is set to true, the remote organization will not be deleted but the resource will be removed from state.")
		return
	}

	//return error here IMMEDIATELY, as Organizations are not allowed to be deleted
	tflog.Error(ctx, "Organizations cannot be deleted")
	resp.Diagnostics.AddError("Organizations cannot be deleted",
		"Foundry does not support deleted Organizations!")
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
