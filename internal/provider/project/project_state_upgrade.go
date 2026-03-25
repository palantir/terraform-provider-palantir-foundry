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

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// UpgradeState handles state migration from older schema versions.
func (r *projectResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: projectV0ToV1StateUpgrader,
			PriorSchema:   projectV0Schema(),
		},
	}
}

func projectV0ToV1StateUpgrader(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	// Read all existing scalar attributes
	var rid, displayName, spaceRid, description, trashStatus *string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("rid"), &rid)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("display_name"), &displayName)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("space_rid"), &spaceRid)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("description"), &description)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("trash_status"), &trashStatus)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read initial_organizations
	var initialOrganizations types.Set
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("initial_organizations"), &initialOrganizations)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Read initial_resource_roles (the old V0 field)
	var rawRoles []struct {
		ResourceRolePrincipal struct {
			Type          string  `tfsdk:"type"`
			PrincipalID   *string `tfsdk:"principal_id"`
			PrincipalType *string `tfsdk:"principal_type"`
		} `tfsdk:"resource_role_principal"`
		RoleID string `tfsdk:"role_id"`
	}
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("initial_resource_roles"), &rawRoles)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Filter to only principalWithId entries (drop everyone entries)
	var principalEntries []principalRoleEntry
	for _, role := range rawRoles {
		if role.ResourceRolePrincipal.Type == "principalWithId" && role.ResourceRolePrincipal.PrincipalID != nil {
			principalType := "USER"
			if role.ResourceRolePrincipal.PrincipalType != nil {
				principalType = *role.ResourceRolePrincipal.PrincipalType
			}
			principalEntries = append(principalEntries, principalRoleEntry{
				RoleID:        role.RoleID,
				PrincipalID:   *role.ResourceRolePrincipal.PrincipalID,
				PrincipalType: principalType,
			})
		}
	}

	principalRolesMap, err := buildPrincipalRolesMap(principalEntries)
	if err != nil {
		resp.Diagnostics.AddError("Failed to upgrade state", err.Error())
		return
	}

	// Set all attributes in the new V1 state
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rid"), rid)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("display_name"), displayName)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("space_rid"), spaceRid)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("description"), description)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("trash_status"), trashStatus)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("initial_principal_roles"), principalRolesMap)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("initial_organizations"), initialOrganizations)...)
}

func projectV0Schema() *schema.Schema {
	return &schema.Schema{
		Attributes: map[string]schema.Attribute{
			"rid":          schema.StringAttribute{Computed: true},
			"display_name": schema.StringAttribute{Required: true},
			"space_rid":    schema.StringAttribute{Required: true},
			"description":  schema.StringAttribute{Optional: true},
			"trash_status": schema.StringAttribute{Computed: true},
			"initial_resource_roles": schema.SetNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"resource_role_principal": schema.SingleNestedAttribute{
							Required: true,
							Attributes: map[string]schema.Attribute{
								"type":           schema.StringAttribute{Required: true},
								"principal_id":   schema.StringAttribute{Optional: true},
								"principal_type": schema.StringAttribute{Optional: true},
							},
						},
						"role_id": schema.StringAttribute{Required: true},
					},
				},
			},
			"initial_organizations": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}
