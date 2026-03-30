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

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource                = &organizationGuestMembershipResource{}
	_ resource.ResourceWithConfigure   = &organizationGuestMembershipResource{}
	_ resource.ResourceWithImportState = &organizationGuestMembershipResource{}
)

// NewOrganizationGuestMembershipResource is a helper function to simplify provider implementation.
func NewOrganizationGuestMembershipResource() resource.Resource {
	return &organizationGuestMembershipResource{}
}

// organizationGuestMembershipResource is the resource implementation.
type organizationGuestMembershipResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider configured client to the resource.
func (r *organizationGuestMembershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *organizationGuestMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_guest_membership"
}

// Schema defines the schema for the resource.
func (r *organizationGuestMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Organization's Guest Members.",
		Attributes: map[string]schema.Attribute{
			"organization_rid": schema.StringAttribute{
				Description: "RID of the Organization.",
				Required:    true,
			},
			"organization_guest_members": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "List of the IDs of the guest members (Users or Groups) of this Organization.",
				Optional:    true,
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *organizationGuestMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan organizationGuestMembershipResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	if !plan.GuestMembers.IsNull() {
		err := r.CreateGuestMembers(ctx, resp, &plan)
		if err != nil {
			resp.Diagnostics.AddError("Error creating the Organization guest members. Please fix your plan if needed and re-apply.", err.Error())
		}
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *organizationGuestMembershipResource) CreateGuestMembers(ctx context.Context, resp *resource.CreateResponse, plan *organizationGuestMembershipResourceModel) error {
	oldGuestMembers, err := r.ReadGuestMembersOnCreation(ctx, resp, plan)

	if err != nil {
		return err
	}

	var newGuestMembers []string

	//only initialize if not null, otherwise ElementsAs will throw error instead of just handling as empty slice
	if !plan.GuestMembers.IsNull() {
		diags := plan.GuestMembers.ElementsAs(ctx, &newGuestMembers, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert guest members to Go slice")
		}
	}

	if !slices.Equal(oldGuestMembers, newGuestMembers) {
		organizationRid := plan.OrganizationRID.ValueString()

		membersToAdd, membersToRemove := helper.FindStringSliceDiff(oldGuestMembers, newGuestMembers)
		if len(membersToAdd) != 0 {
			err := r.AddGuestMembers(ctx, membersToAdd, organizationRid)
			if err != nil {
				return err
			}
		}
		if len(membersToRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveGuestMembers(ctx, membersToRemove, organizationRid)
			if err != nil {
				return err
			}
		} else if len(membersToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found guest members in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, member-removal operations will not be applied.")
		}
	}
	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *organizationGuestMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state organizationGuestMembershipResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadGuestMembers(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Organization guest members", err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *organizationGuestMembershipResource) ReadGuestMembers(ctx context.Context, state *organizationGuestMembershipResourceModel) error {
	organizationRid := state.OrganizationRID.ValueString()
	preview := true
	httpResp, err := r.client.AdminListOrganizationGuestMembers(ctx, organizationRid, &v2.AdminListOrganizationGuestMembersParams{Preview: &preview})

	if err != nil {
		return fmt.Errorf("AdminListOrganizationGuestMembers request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminListOrganizationGuestMembers response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminListOrganizationGuestMembers: %w", err)
	}

	var httpGuestMembersResponseBody v2.AdminListOrganizationGuestMembersResponse
	if err := json.Unmarshal(bodyBytes, &httpGuestMembersResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	guestMemberIds := make([]string, 0)

	if httpGuestMembersResponseBody.Data != nil {
		for _, guestMember := range *httpGuestMembersResponseBody.Data {
			guestMemberIds = append(guestMemberIds, guestMember.PrincipalID.String())
		}
	}

	state.GuestMembers, _ = types.SetValueFrom(ctx, types.StringType, guestMemberIds)
	return nil
}

func (r *organizationGuestMembershipResource) ReadGuestMembersOnCreation(ctx context.Context, resp *resource.CreateResponse, plan *organizationGuestMembershipResourceModel) ([]string, error) {
	organizationRid := plan.OrganizationRID.ValueString()
	preview := true
	httpResp, err := r.client.AdminListOrganizationGuestMembers(ctx, organizationRid, &v2.AdminListOrganizationGuestMembersParams{Preview: &preview})

	if err != nil {
		return nil, fmt.Errorf("AdminListOrganizationGuestMembers request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return nil, fmt.Errorf("failed to format error logging from AdminListOrganizationGuestMembers response: %w", err)
		}
		return nil, errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response from AdminListOrganizationGuestMembers: %w", err)
	}

	var httpGuestMembersResponseBody v2.AdminListOrganizationGuestMembersResponse
	if err := json.Unmarshal(bodyBytes, &httpGuestMembersResponseBody); err != nil {
		return nil, fmt.Errorf("error decoding response: %w", err)
	}

	guestMemberIds := make([]string, 0)

	if httpGuestMembersResponseBody.Data != nil {
		for _, guestMember := range *httpGuestMembersResponseBody.Data {
			guestMemberIds = append(guestMemberIds, guestMember.PrincipalID.String())
		}
	}

	return guestMemberIds, nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *organizationGuestMembershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan organizationGuestMembershipResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state organizationGuestMembershipResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateGuestMembers(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Organization guest members. Please fix your plan if needed and re-apply", err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *organizationGuestMembershipResource) UpdateGuestMembers(ctx context.Context, plan *organizationGuestMembershipResourceModel, state *organizationGuestMembershipResourceModel, resp *resource.UpdateResponse) error {
	var oldGuestMembers []string
	var newGuestMembers []string

	if !state.GuestMembers.IsNull() {
		diags := state.GuestMembers.ElementsAs(ctx, &oldGuestMembers, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert guest members to Go slice")
		}
	}

	//only initialize if not null, otherwise ElementsAs will throw error instead of just handling as empty slice
	if !plan.GuestMembers.IsNull() {
		diags := plan.GuestMembers.ElementsAs(ctx, &newGuestMembers, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert guest members to Go slice")
		}
	}

	if !slices.Equal(oldGuestMembers, newGuestMembers) {
		organizationRid := plan.OrganizationRID.ValueString()

		membersToAdd, membersToRemove := helper.FindStringSliceDiff(oldGuestMembers, newGuestMembers)
		if len(membersToAdd) != 0 {
			err := r.AddGuestMembers(ctx, membersToAdd, organizationRid)
			if err != nil {
				return err
			}
		}
		if len(membersToRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveGuestMembers(ctx, membersToRemove, organizationRid)
			if err != nil {
				return err
			}
		} else if len(membersToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found guest members in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, member-removal operations will not be applied.")
		}
		//if there was a change (and no error thrown), update state to equal plan
		state.GuestMembers = plan.GuestMembers
	}
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *organizationGuestMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state organizationGuestMembershipResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning("Called Delete on organization guest members resource.",
		fmt.Sprintf("The guest membership resource for organization %s will be removed from state, but no members will be removed remotely.", state.OrganizationRID.ValueString()))

}

// ImportState imports an existing organization's guest members into Terraform state.
func (r *organizationGuestMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
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

	tflog.Info(ctx, fmt.Sprintf("Importing organization guest members for organization RID %s", organizationRID))

	// Set the organization RID in state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_rid"), organizationRID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the organization RID
}

func (r *organizationGuestMembershipResource) AddGuestMembers(ctx context.Context, membersToAdd []string, organizationRid string) error {
	uuidsToAdd, err := helper.ConvertStringsToUUIDs(membersToAdd)
	if err != nil {
		return fmt.Errorf("failed to convert members to add to UUIDs: %w", err)
	}

	preview := true
	httpResp, err := r.client.AdminAddOrganizationGuestMembers(ctx, organizationRid, &v2.AdminAddOrganizationGuestMembersParams{Preview: &preview}, v2.AdminAddOrganizationGuestMembersJSONRequestBody{
		PrincipalIds: &uuidsToAdd,
	})

	if err != nil {
		return fmt.Errorf("AdminAddOrganizationGuestMembers request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminAddOrganizationGuestMembers response: %w", err)
		}
		return errors.New(returnString)
	}

	return nil
}

func (r *organizationGuestMembershipResource) RemoveGuestMembers(ctx context.Context, membersToRemove []string, organizationRid string) error {
	uuidsToRemove, err := helper.ConvertStringsToUUIDs(membersToRemove)
	if err != nil {
		return fmt.Errorf("failed to convert members to remove to UUIDs: %w", err)
	}

	preview := true
	httpResp, err := r.client.AdminRemoveOrganizationGuestMembers(ctx, organizationRid, &v2.AdminRemoveOrganizationGuestMembersParams{Preview: &preview}, v2.AdminRemoveOrganizationGuestMembersJSONRequestBody{
		PrincipalIds: &uuidsToRemove,
	})

	if err != nil {
		return fmt.Errorf("AdminRemoveOrganizationGuestMembers request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminRemoveOrganizationGuestMembers response: %w", err)
		}
		return errors.New(returnString)
	}

	return nil
}
