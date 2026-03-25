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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
)

// UpgradeState handles state migration from older schema versions.
func (r *markingCategoryResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: markingCategoryV0ToV1StateUpgrader,
			PriorSchema:   markingCategoryV0Schema(),
		},
	}
}

// markingCategoryV0PermissionsModel is the v0 schema representation of initial_permissions
// where roles is a set of {role, principal_id} objects.
type markingCategoryV0PermissionsModel struct {
	IsPublic         types.Bool `tfsdk:"is_public"`
	OrganizationRids types.Set  `tfsdk:"organization_rids"`
	Roles            types.Set  `tfsdk:"roles"`
}

// markingCategoryV0Model is the v0 schema representation of the marking category resource.
type markingCategoryV0Model struct {
	ID                 types.String                       `tfsdk:"id"`
	Name               types.String                       `tfsdk:"name"`
	Description        types.String                       `tfsdk:"description"`
	CategoryType       types.String                       `tfsdk:"category_type"`
	MarkingType        types.String                       `tfsdk:"marking_type"`
	CreatedBy          types.String                       `tfsdk:"created_by"`
	CreatedTime        types.String                       `tfsdk:"created_time"`
	InitialPermissions *markingCategoryV0PermissionsModel `tfsdk:"initial_permissions"`
}

func markingCategoryV0ToV1StateUpgrader(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var v0 markingCategoryV0Model
	resp.Diagnostics.Append(req.State.Get(ctx, &v0)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert roles from set of objects to map
	var rawAssignments []struct {
		Role        string `tfsdk:"role"`
		PrincipalID string `tfsdk:"principal_id"`
	}
	resp.Diagnostics.Append(v0.InitialPermissions.Roles.ElementsAs(ctx, &rawAssignments, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entries := make([]helper.RoleAssignmentEntry, len(rawAssignments))
	for i, a := range rawAssignments {
		entries[i] = helper.RoleAssignmentEntry{RoleIdentifier: a.Role, PrincipalID: a.PrincipalID}
	}

	newMap, err := helper.BuildRoleAssignmentMap(ctx, entries)
	if err != nil {
		resp.Diagnostics.AddError("Failed to upgrade state", err.Error())
		return
	}

	// Build v1 state reusing the existing model types from models.go
	v1 := markingCategoryResourceModel{
		ID:           v0.ID,
		Name:         v0.Name,
		Description:  v0.Description,
		CategoryType: v0.CategoryType,
		MarkingType:  v0.MarkingType,
		CreatedBy:    v0.CreatedBy,
		CreatedTime:  v0.CreatedTime,
		InitialPermissions: &markingCategoryInitialPermissionsModel{
			IsPublic:         v0.InitialPermissions.IsPublic,
			OrganizationRids: v0.InitialPermissions.OrganizationRids,
			Roles:            newMap,
		},
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, v1)...)
}

func markingCategoryV0Schema() *schema.Schema {
	return &schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true},
			"name":          schema.StringAttribute{Required: true},
			"description":   schema.StringAttribute{Required: true},
			"category_type": schema.StringAttribute{Computed: true},
			"marking_type":  schema.StringAttribute{Computed: true},
			"created_by":    schema.StringAttribute{Computed: true},
			"created_time":  schema.StringAttribute{Computed: true},
			"initial_permissions": schema.SingleNestedAttribute{
				Required: true,
				Attributes: map[string]schema.Attribute{
					"is_public": schema.BoolAttribute{Required: true},
					"organization_rids": schema.SetAttribute{
						Required:    true,
						ElementType: types.StringType,
					},
					"roles": schema.SetAttribute{
						Required: true,
						ElementType: types.ObjectType{
							AttrTypes: map[string]attr.Type{
								"role":         types.StringType,
								"principal_id": types.StringType,
							},
						},
					},
				},
			},
		},
	}
}
