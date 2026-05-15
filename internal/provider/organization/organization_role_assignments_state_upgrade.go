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

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
)

// UpgradeState handles state migration from older schema versions.
func (r *organizationRoleAssignmentsResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: v0ToV1StateUpgrader,
			PriorSchema:   V0Schema(),
		},
	}
}

func v0ToV1StateUpgrader(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var organizationRID types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("organization_rid"), &organizationRID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var rawAssignments []struct {
		RoleID      string `tfsdk:"role_id"`
		PrincipalID string `tfsdk:"principal_id"`
	}
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("organization_role_assignments"), &rawAssignments)...)
	if resp.Diagnostics.HasError() {
		return
	}

	entries := make([]helper.RoleAssignmentEntry, len(rawAssignments))
	for i, a := range rawAssignments {
		entries[i] = helper.RoleAssignmentEntry{RoleIdentifier: a.RoleID, PrincipalID: a.PrincipalID}
	}

	newMap, err := helper.BuildRoleAssignmentMap(ctx, entries)
	if err != nil {
		resp.Diagnostics.AddError("Failed to upgrade state", err.Error())
		return
	}

	v1 := organizationRoleAssignmentsResourceModel{
		OrganizationRID:             organizationRID,
		OrganizationRoleAssignments: newMap,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, v1)...)
}

func V0Schema() *schema.Schema {
	return &schema.Schema{
		Attributes: map[string]schema.Attribute{
			"organization_rid": schema.StringAttribute{Required: true},
			"organization_role_assignments": schema.SetAttribute{
				Required: true,
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
