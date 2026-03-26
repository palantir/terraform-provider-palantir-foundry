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

// markingV0Model is the v0 schema representation where initial_role_assignments
// is a set of {role, principal_id} objects.
type markingV0Model struct {
	ID                     types.String `tfsdk:"id"`
	CategoryID             types.String `tfsdk:"category_id"`
	Name                   types.String `tfsdk:"name"`
	Description            types.String `tfsdk:"description"`
	InitialRoleAssignments types.Set    `tfsdk:"initial_role_assignments"`
}

// UpgradeState handles state migration from older schema versions.
func (r *markingResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: markingV0ToV1StateUpgrader,
			PriorSchema:   markingV0Schema(),
		},
	}
}

func markingV0ToV1StateUpgrader(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var v0 markingV0Model
	resp.Diagnostics.Append(req.State.Get(ctx, &v0)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Convert roles from set of objects to map
	var rawAssignments []struct {
		Role        string `tfsdk:"role"`
		PrincipalID string `tfsdk:"principal_id"`
	}
	resp.Diagnostics.Append(v0.InitialRoleAssignments.ElementsAs(ctx, &rawAssignments, false)...)
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

	v1 := markingResourceModel{
		ID:                     v0.ID,
		CategoryID:             v0.CategoryID,
		Name:                   v0.Name,
		Description:            v0.Description,
		InitialRoleAssignments: newMap,
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, v1)...)
}

func markingV0Schema() *schema.Schema {
	return &schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true},
			"category_id": schema.StringAttribute{Required: true},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"initial_role_assignments": schema.SetAttribute{
				Optional: true,
				ElementType: types.ObjectType{
					AttrTypes: map[string]attr.Type{
						"role":         types.StringType,
						"principal_id": types.StringType,
					},
				},
			},
		},
	}
}
