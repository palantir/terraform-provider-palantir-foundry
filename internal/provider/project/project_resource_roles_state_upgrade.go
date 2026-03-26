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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// UpgradeState handles state migration from older schema versions.
func (r *projectResourceRolesResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: projectResourceRolesV0ToV1StateUpgrader,
			PriorSchema:   projectResourceRolesV0Schema(),
		},
	}
}

func projectResourceRolesV0ToV1StateUpgrader(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var projectRid string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project_rid"), &projectRid)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var rawRoles []struct {
		ResourceRolePrincipal struct {
			Type          string  `tfsdk:"type"`
			PrincipalID   *string `tfsdk:"principal_id"`
			PrincipalType *string `tfsdk:"principal_type"`
		} `tfsdk:"resource_role_principal"`
		RoleID string `tfsdk:"role_id"`
	}
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("project_resource_roles"), &rawRoles)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var principalEntries []principalRoleEntry
	var defaultRoleIDs []string

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
		} else if role.ResourceRolePrincipal.Type == "everyone" {
			defaultRoleIDs = append(defaultRoleIDs, role.RoleID)
		}
	}

	principalRolesMap, err := buildPrincipalRolesMap(principalEntries)
	if err != nil {
		resp.Diagnostics.AddError("Failed to upgrade state", err.Error())
		return
	}

	defaultRoleValues := make([]attr.Value, len(defaultRoleIDs))
	for i, roleID := range defaultRoleIDs {
		defaultRoleValues[i] = types.StringValue(roleID)
	}
	defaultRolesSet, diags := types.SetValue(types.StringType, defaultRoleValues)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("project_rid"), projectRid)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("principal_roles"), principalRolesMap)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("default_roles"), defaultRolesSet)...)
}

func projectResourceRolesV0Schema() *schema.Schema {
	return &schema.Schema{
		Attributes: map[string]schema.Attribute{
			"project_rid": schema.StringAttribute{Required: true},
			"project_resource_roles": schema.SetNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"resource_role_principal": schema.SingleNestedAttribute{
							Required: true,
							Attributes: map[string]schema.Attribute{
								"type": schema.StringAttribute{
									Required: true,
								},
								"principal_id": schema.StringAttribute{
									Optional: true,
								},
								"principal_type": schema.StringAttribute{
									Optional: true,
								},
							},
						},
						"role_id": schema.StringAttribute{
							Required: true,
						},
					},
				},
			},
		},
	}
}
