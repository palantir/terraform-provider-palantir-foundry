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
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
)

// UpgradeState handles state migration from older schema versions.
func (r *markingRoleAssignmentsResource) UpgradeState(_ context.Context) map[int64]resource.StateUpgrader {
	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: markingRoleAssignmentsV0ToV1StateUpgrader,
			PriorSchema:   markingRoleAssignmentsV0Schema(),
		},
	}
}

func markingRoleAssignmentsV0ToV1StateUpgrader(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
	var markingID string
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("marking_id"), &markingID)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var rawAssignments []struct {
		Role        string `tfsdk:"role"`
		PrincipalID string `tfsdk:"principal_id"`
	}
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("marking_role_assignments"), &rawAssignments)...)
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

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("marking_id"), markingID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("marking_role_assignments"), newMap)...)
}

func markingRoleAssignmentsV0Schema() *schema.Schema {
	return &schema.Schema{
		Attributes: map[string]schema.Attribute{
			"marking_id": schema.StringAttribute{Required: true},
			"marking_role_assignments": schema.SetAttribute{
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
