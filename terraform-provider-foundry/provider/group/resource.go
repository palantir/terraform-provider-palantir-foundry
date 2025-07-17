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

package group

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	v2 "github.com/ericanderson/foundry-terraform/gateway-client/v2"
	providerError "github.com/ericanderson/foundry-terraform/terraform-provider-foundry/provider/errors"
	"github.com/ericanderson/foundry-terraform/terraform-provider-foundry/provider/helper"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource              = &groupResource{}
	_ resource.ResourceWithConfigure = &groupResource{}
)

// NewGroupResource is a helper function to simplify provider implementation.
func NewGroupResource() resource.Resource {
	return &groupResource{}
}

// groupResource is the resource implementation.
type groupResource struct {
	client *v2.ClientWithResponses
}

// Configure adds the provider configured client to the resource.
func (r *groupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *groupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group"
}

// Schema defines the schema for the resource.
func (r *groupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Identifier of the group.",
				Computed:    true,
			},
			"name": schema.StringAttribute{
				Description: "Name of the group.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the group.",
				Optional:    true,
			},
			"organizations": schema.ListAttribute{
				ElementType: types.StringType,
				Description: "List of organizations the group is under.",
				Required:    true,
			},
			"realm": schema.StringAttribute{
				Description: "Realm of the group.",
				Computed:    true,
			},
			"planned_group_members": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "Planned list of members in this group",
				Required:    true,
			},
			"group_members": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "Actual list of members in this group, computed after successful addition/removal of group members.",
				Computed:    true,
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *groupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}

	err := r.CreateGroup(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the group resource",
			"Error creating the group resource itself. Since this is the primary resource, nothing has been provisioned and we can safely return")
		return
	}

	err = r.CreateGroupMembers(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddWarning("Error creating the group members",
			err.Error())
		resp.Diagnostics.AddWarning("Please fix your plan if needed and re-apply.",
			"We are throwing a warning here to ensure previous changes are not lost. Please fix your plan if needed and re-apply.")
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *groupResource) CreateGroup(ctx context.Context, resp *resource.CreateResponse, plan *groupResourceModel) error {
	var organizationsGoSlice []v2.CoreOrganizationRid
	diags := plan.Organizations.ElementsAs(context.Background(), &organizationsGoSlice, false)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return fmt.Errorf("failed to convert organizations to Go slice")
	}

	description := plan.Description.ValueString()

	httpResp, err := r.client.AdminCreateGroup(ctx, v2.AdminCreateGroupJSONRequestBody{
		Name:          plan.Name.ValueString(),
		Description:   &description,
		Organizations: &organizationsGoSlice,
	})

	if err != nil {
		resp.Diagnostics.AddError("AdminCreateGroup request failed", err.Error())
		return fmt.Errorf("AdminCreateGroup request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminCreateGroup response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminCreateGroup response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminCreateGroup was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminCreateGroup was unsuccessful: %s", returnString)
	}

	//read body and then close
	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminCreateGroup", err.Error())
		return fmt.Errorf("failed to parse response from AdminCreateGroup: %w", err)
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

	//update state for computed values

	plan.ID = types.StringValue(httpResponseBody.ID)
	plan.Realm = types.StringValue(httpResponseBody.Realm)
	return nil
}

func (r *groupResource) CreateGroupMembers(ctx context.Context, resp *resource.CreateResponse, plan *groupResourceModel) error {
	var plannedGroupMembers []v2.CorePrincipalID
	diags := plan.PlannedGroupMembers.ElementsAs(ctx, &plannedGroupMembers, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert planned group members to Go slice")
	}

	httpResp, err := r.client.AdminAddGroupMembers(ctx, plan.ID.ValueString(), v2.AdminAddGroupMembersJSONRequestBody{
		PrincipalIds: &plannedGroupMembers,
	})

	if err != nil {
		return fmt.Errorf("AdminAddGroupMembers request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminAddGroupMembers response: %w", err)
		}
		plan.GroupMembers, diags = types.SetValueFrom(ctx, types.StringType, make([]string, 0))
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return fmt.Errorf("failed to initialize group members in plan")
		}
		return errors.New(returnString)
	}
	plan.GroupMembers = plan.PlannedGroupMembers
	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *groupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state groupResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadGroup(ctx, resp, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the group resource",
			"Error reading the group resource itself. Since this is the primary resource, nothing has been changed and we can safely return")
		return
	}
	err = r.ReadGroupMembers(ctx, resp, &state)
	if err != nil {
		resp.Diagnostics.AddWarning("Error reading the group members",
			err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *groupResource) ReadGroup(ctx context.Context, resp *resource.ReadResponse, state *groupResourceModel) error {
	httpResp, err := r.client.AdminGetGroup(ctx, state.ID.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("AdminGetGroup request failed", err.Error())
		return fmt.Errorf("AdminGetGroup request failed: %w", err)
	}
	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from AdminGetGroup response", err.Error())
			return fmt.Errorf("failed to format error logging from AdminGetGroup response: %w", err)
		}
		resp.Diagnostics.AddError("Response from AdminGetGroup was unsuccessful: ", returnString)
		return fmt.Errorf("response from AdminGetGroup was unsuccessful: %s", returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from AdminGetGroup", err.Error())
		return fmt.Errorf("failed to parse response from AdminGetGroup: %w", err)
	}

	var httpResponseBody responseBody
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	//if success - take id from the response and update the state

	state.ID = types.StringValue(httpResponseBody.ID)
	state.Name = types.StringValue(httpResponseBody.Name)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)
	state.Realm = types.StringValue(httpResponseBody.Realm)
	state.Organizations, _ = types.ListValueFrom(ctx, types.StringType, httpResponseBody.Organizations)
	return nil
}

func (r *groupResource) ReadGroupMembers(ctx context.Context, resp *resource.ReadResponse, state *groupResourceModel) error {
	httpResp, err := r.client.AdminListGroupMembers(ctx, state.ID.ValueString(), &v2.AdminListGroupMembersParams{
		//TODO: add pagination support and transitive support if needed
	})

	if err != nil {
		return fmt.Errorf("AdminListGroupMembers request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from AdminListGroupMembers response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from AdminListGroupMembers: %w", err)
	}

	var httpGroupMembersResponseBody groupMembersResponseBody
	if err := json.Unmarshal(bodyBytes, &httpGroupMembersResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	groupMemberIds := make([]string, 0)

	//var groupMemberIds []string
	for _, groupMember := range httpGroupMembersResponseBody.Data {
		groupMemberIds = append(groupMemberIds, groupMember.PrincipalID)
	}

	state.GroupMembers, _ = types.SetValueFrom(ctx, types.StringType, groupMemberIds)

	//bit hacky but we need to update the planned group members as well in state to keep state = plan
	state.PlannedGroupMembers = state.GroupMembers
	return nil
}

// Update updates the resource and sets the updated Terraform state on success.
// TODO: add updating group to API-GATEWAY and implement here. Right now we are just handling group members here
func (r *groupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan groupResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state groupResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateGroupMembers(ctx, resp, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddWarning("Error updating the group members",
			err.Error())
		resp.Diagnostics.AddWarning("Please fix your plan if needed and re-apply.",
			"We are throwing a warning here to ensure previous changes are not lost. Please fix your plan if needed and re-apply.")
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *groupResource) UpdateGroupMembers(ctx context.Context, resp *resource.UpdateResponse, plan *groupResourceModel, state *groupResourceModel) error {
	var oldGroupMembers []string
	var newGroupMembers []string

	diags := state.GroupMembers.ElementsAs(ctx, &oldGroupMembers, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert group members to Go slice")
	}

	diags = plan.PlannedGroupMembers.ElementsAs(ctx, &newGroupMembers, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert planned group members to Go slice")
	}

	if !slices.Equal(oldGroupMembers, newGroupMembers) {
		// Determine members to add and remove
		membersToAdd, membersToRemove := helper.FindStringSliceDiff(oldGroupMembers, newGroupMembers)
		if len(membersToAdd) != 0 {
			//create body
			httpResp, err := r.client.AdminAddGroupMembers(ctx, state.ID.ValueString(), v2.AdminAddGroupMembersJSONRequestBody{
				PrincipalIds: &membersToAdd,
			})

			if err != nil {
				return fmt.Errorf("AdminAddGroupMembers request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminAddGroupMembers response: %w", err)
				}
				if plan.GroupMembers.IsUnknown() {
					plan.GroupMembers = state.GroupMembers
				}
				state.PlannedGroupMembers = plan.PlannedGroupMembers
				return errors.New(returnString)
			}
			plan.GroupMembers = plan.PlannedGroupMembers
		}
		if len(membersToRemove) != 0 {
			//create body
			httpResp, err := r.client.AdminRemoveGroupMembers(ctx, state.ID.ValueString(), v2.AdminRemoveGroupMembersRequest{
				PrincipalIds: &membersToRemove,
			})

			if err != nil {
				return fmt.Errorf("AdminRemoveGroupMembers request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from AdminRemoveGroupMembers response: %w", err)
				}
				if plan.GroupMembers.IsUnknown() {
					plan.GroupMembers = state.GroupMembers
				}
				state.PlannedGroupMembers = plan.PlannedGroupMembers
				return errors.New(returnString)
			}
			plan.GroupMembers = plan.PlannedGroupMembers
		}
		state.GroupMembers = plan.GroupMembers
	}
	state.PlannedGroupMembers = plan.PlannedGroupMembers
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *groupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state groupResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	httpResp, err := r.client.AdminDeleteGroup(ctx, state.ID.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("Request failed", err.Error())
		return
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("failed to format error logging from AdminAddGroupMembers response", err.Error())
		}
		resp.Diagnostics.AddError("Request failed", returnString)
		//make sure we return here so don't update state to uphold Terraform best practices
		return
	}
}

// ImportState imports an existing group into Terraform state.
func (r *groupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is expected to be the group ID
	groupID := req.ID

	// Validate the ID format (optional, can add your own validation logic)
	if groupID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the group ID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing group with ID %s", groupID))

	// Set the organization RID in state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), groupID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the RID
}
