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

package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"

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
	_ resource.Resource                 = &projectResourceRolesResource{}
	_ resource.ResourceWithConfigure    = &projectResourceRolesResource{}
	_ resource.ResourceWithUpgradeState = &projectResourceRolesResource{}
)

// NewProjectResourceRolesResource is a helper function to simplify provider implementation.
func NewProjectResourceRolesResource() resource.Resource {
	return &projectResourceRolesResource{}
}

// projectResourceRolesResource is the resource implementation.
type projectResourceRolesResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider configured client to the resource.
func (r *projectResourceRolesResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
func (r *projectResourceRolesResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_resource_roles"
}

// Schema defines the schema for the resource.
func (r *projectResourceRolesResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Project's Resource Roles.",
		Version:     1,
		Attributes: map[string]schema.Attribute{
			"project_rid": schema.StringAttribute{
				Description: "RID of the Project.",
				Required:    true,
			},
			"principal_roles": principalRolesMapSchema(
				"Map of Role ID to groups and users for this Project. " +
					"Only applies to roles assigned to specific users or groups (principalWithId)."),
			"default_roles": schema.SetAttribute{
				Description: "Set of Role IDs applied to everyone for this Project.",
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *projectResourceRolesResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectResourceRolesResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.CreateProjectResourceRoles(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the Project Resource Roles. Please fix your plan if needed and re-apply.", err.Error())
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *projectResourceRolesResource) CreateProjectResourceRoles(ctx context.Context, resp *resource.CreateResponse, plan *projectResourceRolesResourceModel) error {
	oldPrincipalEntries, oldDefaultRoles, err := r.readResourceRolesRaw(ctx, plan.ProjectRid.ValueString())
	if err != nil {
		return fmt.Errorf("failed to read project roles on creation: %w", err)
	}

	// Handle principal roles
	if !plan.PrincipalRoles.IsNull() {
		newPrincipalEntries, err := flattenPrincipalRolesMap(ctx, plan.PrincipalRoles)
		if err != nil {
			return fmt.Errorf("failed to convert planned principal_roles: %w", err)
		}

		toAdd, toRemove := findPrincipalRolesDiff(oldPrincipalEntries, newPrincipalEntries)
		if len(toAdd) != 0 {
			err := r.AddProjectResourceRoles(ctx, principalEntriesToResourceRoles(toAdd), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		}
		if len(toRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveProjectResourceRoles(ctx, principalEntriesToResourceRoles(toRemove), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		} else if len(toRemove) != 0 {
			resp.Diagnostics.AddWarning("Found Resource Roles defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, Resource Roles removal operations will not be applied.")
		}
	}

	// Handle default roles
	if !plan.DefaultRoles.IsNull() {
		var newDefaultRoles []string
		diags := plan.DefaultRoles.ElementsAs(ctx, &newDefaultRoles, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert planned default_roles")
		}

		defaultToAdd, defaultToRemove := diffStringSlices(oldDefaultRoles, newDefaultRoles)
		if len(defaultToAdd) != 0 {
			err := r.AddProjectResourceRoles(ctx, defaultRolesToResourceRoles(defaultToAdd), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		}
		if len(defaultToRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveProjectResourceRoles(ctx, defaultRolesToResourceRoles(defaultToRemove), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		} else if len(defaultToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found default Resource Roles defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, Resource Roles removal operations will not be applied.")
		}
	}

	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *projectResourceRolesResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state projectResourceRolesResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadProjectResourceRoles(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Project resource_roles", err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *projectResourceRolesResource) ReadProjectResourceRoles(ctx context.Context, state *projectResourceRolesResourceModel) error {
	principalEntries, defaultRoles, err := r.readResourceRolesRaw(ctx, state.ProjectRid.ValueString())
	if err != nil {
		return err
	}

	roleMap, err := buildPrincipalRolesMap(principalEntries)
	if err != nil {
		return fmt.Errorf("failed to build principal roles map: %w", err)
	}
	state.PrincipalRoles = roleMap

	defaultRoleValues := make([]attr.Value, len(defaultRoles))
	for i, roleID := range defaultRoles {
		defaultRoleValues[i] = types.StringValue(roleID)
	}
	defaultRolesSet, diags := types.SetValue(types.StringType, defaultRoleValues)
	if diags.HasError() {
		return fmt.Errorf("failed to build default roles set")
	}
	state.DefaultRoles = defaultRolesSet

	return nil
}

// readResourceRolesRaw fetches resource roles from the API and separates them into
// principal entries (for principalWithId) and default role IDs (for everyone).
func (r *projectResourceRolesResource) readResourceRolesRaw(ctx context.Context, projectRid string) ([]principalRoleEntry, []string, error) {
	pageSize := constants.PageSize
	filesystemListResourceRolesParams := v2.FilesystemListResourceRolesParams{PageSize: &pageSize}
	httpResp, err := r.client.FilesystemListResourceRoles(ctx, projectRid, &filesystemListResourceRolesParams)

	if err != nil {
		return nil, nil, fmt.Errorf("FilesystemListResourceRoles request failed: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to format error logging from FilesystemListResourceRoles response: %w", err)
		}
		return nil, nil, errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse response from FilesystemListResourceRoles: %w", err)
	}

	var httpListResourceRoles resourceRolesResponse
	if err := json.Unmarshal(bodyBytes, &httpListResourceRoles); err != nil {
		return nil, nil, fmt.Errorf("error decoding response: %w", err)
	}

	var principalEntries []principalRoleEntry
	var defaultRoles []string

	for _, role := range httpListResourceRoles.Roles {
		if role.ResourceRolePrincipal.Type == constants.PrincipalWithID {
			principalEntries = append(principalEntries, principalRoleEntry{
				RoleID:        role.RoleID,
				PrincipalID:   role.ResourceRolePrincipal.PrincipalID,
				PrincipalType: role.ResourceRolePrincipal.PrincipalType,
			})
		} else if role.ResourceRolePrincipal.Type == constants.Everyone {
			defaultRoles = append(defaultRoles, role.RoleID)
		}
	}

	return principalEntries, defaultRoles, nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *projectResourceRolesResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan projectResourceRolesResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state projectResourceRolesResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateProjectResourceRoles(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Project resource_roles. Please fix your plan if needed and re-apply", err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *projectResourceRolesResource) UpdateProjectResourceRoles(ctx context.Context, plan *projectResourceRolesResourceModel, state *projectResourceRolesResourceModel, resp *resource.UpdateResponse) error {
	// Handle principal roles
	if !plan.PrincipalRoles.Equal(state.PrincipalRoles) {
		oldEntries, err := flattenPrincipalRolesMap(ctx, state.PrincipalRoles)
		if err != nil {
			return fmt.Errorf("failed to convert state principal roles: %w", err)
		}
		newEntries, err := flattenPrincipalRolesMap(ctx, plan.PrincipalRoles)
		if err != nil {
			return fmt.Errorf("failed to convert plan principal roles: %w", err)
		}

		toAdd, toRemove := findPrincipalRolesDiff(oldEntries, newEntries)
		if len(toAdd) != 0 {
			err := r.AddProjectResourceRoles(ctx, principalEntriesToResourceRoles(toAdd), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		}
		if len(toRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveProjectResourceRoles(ctx, principalEntriesToResourceRoles(toRemove), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		} else if len(toRemove) != 0 {
			resp.Diagnostics.AddWarning("Found Resource Roles defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, Resource Roles removal operations will not be applied.")
		}
		state.PrincipalRoles = plan.PrincipalRoles
	}

	// Handle default roles
	if !plan.DefaultRoles.Equal(state.DefaultRoles) {
		var oldDefaults, newDefaults []string
		if !state.DefaultRoles.IsNull() {
			diags := state.DefaultRoles.ElementsAs(ctx, &oldDefaults, false)
			if diags.HasError() {
				return fmt.Errorf("failed to convert state default roles")
			}
		}
		if !plan.DefaultRoles.IsNull() {
			diags := plan.DefaultRoles.ElementsAs(ctx, &newDefaults, false)
			if diags.HasError() {
				return fmt.Errorf("failed to convert plan default roles")
			}
		}

		defaultToAdd, defaultToRemove := diffStringSlices(oldDefaults, newDefaults)
		if len(defaultToAdd) != 0 {
			err := r.AddProjectResourceRoles(ctx, defaultRolesToResourceRoles(defaultToAdd), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		}
		if len(defaultToRemove) != 0 && !r.deletionsDisabled {
			err := r.RemoveProjectResourceRoles(ctx, defaultRolesToResourceRoles(defaultToRemove), plan.ProjectRid.ValueString())
			if err != nil {
				return err
			}
		} else if len(defaultToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found default Resource Roles defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, Resource Roles removal operations will not be applied.")
		}
		state.DefaultRoles = plan.DefaultRoles
	}

	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *projectResourceRolesResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Retrieve values from state
	var state projectResourceRolesResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.AddWarning("Called Delete on a project resource_roles resource.",
		fmt.Sprintf("The resource_roles resource for project rid %s will be removed from state, but no resource_roles will be removed remotely.", state.ProjectRid.ValueString()))

}

// ImportState imports existing project resource_roles into Terraform state.
func (r *projectResourceRolesResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is expected to be the project RID
	projectRID := req.ID

	// Validate the ID format (optional, can add your own validation logic)
	if projectRID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the project RID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing project resource_roles for project with ID %s", projectRID))

	// Set the Project RID in state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_rid"), projectRID)...)

	// The Read method will be called automatically after ImportState
	// to refresh all the other attributes based on the RID
}

// resourceRolePayload is used internally to build API request payloads.
type resourceRolePayload struct {
	principalType string // "principalWithId" or "everyone"
	principalID   *string
	roleID        string
}

func (r *projectResourceRolesResource) AddProjectResourceRoles(ctx context.Context, roles []resourceRolePayload, id string) error {
	roleUpdates := make([]v2.FilesystemResourceRoleIdentifier, len(roles))

	for i, role := range roles {
		principal := v2.FilesystemResourceRolePrincipalIdentifier{}
		if role.principalType == constants.PrincipalWithID {
			if role.principalID == nil {
				return fmt.Errorf("principal ID must be provided for principal type %s", constants.PrincipalWithID)
			}
			principalIDAsUUID, err := uuid.Parse(*role.principalID)
			if err != nil {
				return fmt.Errorf("invalid UUID format for principal ID %s: %w", *role.principalID, err)
			}

			err = principal.FromFilesystemPrincipalIDOnly(v2.FilesystemPrincipalIDOnly{
				PrincipalID: principalIDAsUUID,
				Type:        role.principalType,
			})
			if err != nil {
				return fmt.Errorf("FilesystemPrincipalWithID request failed: %w", err)
			}
			roleUpdates[i] = v2.FilesystemResourceRoleIdentifier{
				ResourceRolePrincipal: principal,
				RoleID:                role.roleID,
			}
		}

		if role.principalType == constants.Everyone {
			err := principal.FromFilesystemEveryone(v2.FilesystemEveryone{
				Type: role.principalType,
			})
			if err != nil {
				return fmt.Errorf("FilesystemEveryone request failed: %w", err)
			}
			roleUpdates[i] = v2.FilesystemResourceRoleIdentifier{
				ResourceRolePrincipal: principal,
				RoleID:                role.roleID,
			}
		}
	}

	httpResp, err := r.client.FilesystemAddResourceRoles(ctx, id, v2.FilesystemAddResourceRolesJSONRequestBody{
		Roles: &roleUpdates,
	})

	if err != nil {
		return fmt.Errorf("FilesystemAddResourceRoles request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			log.Fatal(err)
		}
		return errors.New(returnString)
	}

	return nil
}

func (r *projectResourceRolesResource) RemoveProjectResourceRoles(ctx context.Context, roles []resourceRolePayload, id string) error {

	roleUpdates := make([]v2.FilesystemResourceRoleIdentifier, len(roles))

	for i, role := range roles {
		principal := v2.FilesystemResourceRolePrincipalIdentifier{}
		if role.principalType == constants.PrincipalWithID {
			if role.principalID == nil {
				return fmt.Errorf("principal ID must be provided for principal type %s", constants.PrincipalWithID)
			}
			principalIDAsUUID, err := uuid.Parse(*role.principalID)
			if err != nil {
				return fmt.Errorf("invalid UUID format for principal ID %s: %w", *role.principalID, err)
			}

			err = principal.FromFilesystemPrincipalIDOnly(v2.FilesystemPrincipalIDOnly{
				PrincipalID: principalIDAsUUID,
				Type:        role.principalType,
			})
			if err != nil {
				return fmt.Errorf("FilesystemPrincipalWithID request failed: %w", err)
			}
			roleUpdates[i] = v2.FilesystemResourceRoleIdentifier{
				ResourceRolePrincipal: principal,
				RoleID:                role.roleID,
			}
		}

		if role.principalType == constants.Everyone {
			err := principal.FromFilesystemEveryone(v2.FilesystemEveryone{
				Type: role.principalType,
			})
			if err != nil {
				return fmt.Errorf("FilesystemEveryone request failed: %w", err)
			}
			roleUpdates[i] = v2.FilesystemResourceRoleIdentifier{
				ResourceRolePrincipal: principal,
				RoleID:                role.roleID,
			}
		}
	}

	httpResp, err := r.client.FilesystemRemoveResourceRoles(ctx, id, v2.FilesystemRemoveResourceRolesJSONRequestBody{
		Roles: &roleUpdates,
	})

	if err != nil {
		return fmt.Errorf("FilesystemRemoveResourceRoles request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			log.Fatal(err)
		}
		return errors.New(returnString)
	}

	return nil
}

// principalEntriesToResourceRoles converts principalRoleEntry items to resourceRolePayload
// items with type "principalWithId".
func principalEntriesToResourceRoles(entries []principalRoleEntry) []resourceRolePayload {
	result := make([]resourceRolePayload, len(entries))
	for i, e := range entries {
		pid := e.PrincipalID
		result[i] = resourceRolePayload{
			principalType: constants.PrincipalWithID,
			principalID:   &pid,
			roleID:        e.RoleID,
		}
	}
	return result
}

// defaultRolesToResourceRoles converts a list of role IDs to resourceRolePayload items
// with type "everyone".
func defaultRolesToResourceRoles(roleIDs []string) []resourceRolePayload {
	result := make([]resourceRolePayload, len(roleIDs))
	for i, roleID := range roleIDs {
		result[i] = resourceRolePayload{
			principalType: constants.Everyone,
			roleID:        roleID,
		}
	}
	return result
}

// diffStringSlices computes added and removed items between two string slices.
func diffStringSlices(oldSlice, newSlice []string) (added, removed []string) {
	oldSet := make(map[string]bool, len(oldSlice))
	newSet := make(map[string]bool, len(newSlice))

	for _, s := range oldSlice {
		oldSet[s] = true
	}
	for _, s := range newSlice {
		newSet[s] = true
	}

	newKeys := make([]string, 0, len(newSet))
	for k := range newSet {
		newKeys = append(newKeys, k)
	}
	sort.Strings(newKeys)

	oldKeys := make([]string, 0, len(oldSet))
	for k := range oldSet {
		oldKeys = append(oldKeys, k)
	}
	sort.Strings(oldKeys)

	for _, k := range newKeys {
		if !oldSet[k] {
			added = append(added, k)
		}
	}
	for _, k := range oldKeys {
		if !newSet[k] {
			removed = append(removed, k)
		}
	}
	return added, removed
}
