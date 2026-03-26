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
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
)

type markingV0Assignment struct {
	role        string
	principalID string
}

var markingV0EntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"role":         tftypes.String,
		"principal_id": tftypes.String,
	},
}

var markingV0RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"marking_id":               tftypes.String,
		"marking_role_assignments": tftypes.Set{ElementType: markingV0EntryTfType},
	},
}

var markingV1RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"marking_id": tftypes.String,
		"marking_role_assignments": tftypes.Map{
			ElementType: tftypes.Set{ElementType: tftypes.String},
		},
	},
}

func markingTestV1Schema() schema.Schema {
	markingRoleAssignmentsSchema := helper.RoleAssignmentMapSchema("Map of Role to set of Principal IDs.")
	markingRoleAssignmentsSchema.Required = false
	markingRoleAssignmentsSchema.Optional = true

	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"marking_id":               schema.StringAttribute{Required: true},
			"marking_role_assignments": markingRoleAssignmentsSchema,
		},
	}
}

func buildMarkingV0State(markingID string, assignments []markingV0Assignment) tfsdk.State {
	entries := make([]tftypes.Value, len(assignments))
	for i, a := range assignments {
		entries[i] = tftypes.NewValue(markingV0EntryTfType, map[string]tftypes.Value{
			"role":         tftypes.NewValue(tftypes.String, a.role),
			"principal_id": tftypes.NewValue(tftypes.String, a.principalID),
		})
	}

	raw := tftypes.NewValue(markingV0RootTfType, map[string]tftypes.Value{
		"marking_id":               tftypes.NewValue(tftypes.String, markingID),
		"marking_role_assignments": tftypes.NewValue(tftypes.Set{ElementType: markingV0EntryTfType}, entries),
	})

	return tfsdk.State{Schema: markingRoleAssignmentsV0Schema(), Raw: raw}
}

func buildEmptyMarkingV1State() tfsdk.State {
	raw := tftypes.NewValue(markingV1RootTfType, map[string]tftypes.Value{
		"marking_id": tftypes.NewValue(tftypes.String, ""),
		"marking_role_assignments": tftypes.NewValue(
			tftypes.Map{ElementType: tftypes.Set{ElementType: tftypes.String}},
			map[string]tftypes.Value{},
		),
	})

	return tfsdk.State{Schema: markingTestV1Schema(), Raw: raw}
}

func TestMarkingUpgradeStateV0ToV1(t *testing.T) {
	tests := []struct {
		name          string
		markingID     string
		v0Assignments []markingV0Assignment
		expectedRoles map[string][]string // role -> sorted principal_ids
	}{
		{
			name:      "single role single principal",
			markingID: "marking-abc123",
			v0Assignments: []markingV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1"},
			},
		},
		{
			name:      "single role multiple principals are grouped",
			markingID: "marking-abc123",
			v0Assignments: []markingV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
				{role: "ADMINISTER", principalID: "user-2"},
				{role: "ADMINISTER", principalID: "user-3"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1", "user-2", "user-3"},
			},
		},
		{
			name:      "multiple roles with multiple principals",
			markingID: "marking-abc123",
			v0Assignments: []markingV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
				{role: "DECLASSIFY", principalID: "user-2"},
				{role: "ADMINISTER", principalID: "user-3"},
				{role: "DECLASSIFY", principalID: "user-4"},
				{role: "USE", principalID: "user-5"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1", "user-3"},
				"DECLASSIFY": {"user-2", "user-4"},
				"USE":        {"user-5"},
			},
		},
		{
			name:          "empty assignments produces empty map",
			markingID:     "marking-abc123",
			v0Assignments: []markingV0Assignment{},
			expectedRoles: map[string][]string{},
		},
		{
			name:      "marking id is preserved",
			markingID: "marking-some-specific-id-value",
			v0Assignments: []markingV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1"},
			},
		},
	}

	r := &markingRoleAssignmentsResource{}
	ctx := context.Background()
	upgraders := r.UpgradeState(ctx)
	upgrader, ok := upgraders[0]
	if !ok {
		t.Fatal("expected state upgrader for version 0")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqState := buildMarkingV0State(tt.markingID, tt.v0Assignments)
			respState := buildEmptyMarkingV1State()

			req := resource.UpgradeStateRequest{State: &reqState}
			resp := resource.UpgradeStateResponse{State: respState}

			upgrader.StateUpgrader(ctx, req, &resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics errors: %v", resp.Diagnostics.Errors())
			}

			// Verify marking_id is preserved
			var gotID string
			diags := resp.State.GetAttribute(ctx, path.Root("marking_id"), &gotID)
			if diags.HasError() {
				t.Fatalf("failed to read marking_id from upgraded state: %v", diags.Errors())
			}
			if gotID != tt.markingID {
				t.Errorf("marking_id: got %q, want %q", gotID, tt.markingID)
			}

			// Verify role assignments map
			var gotMap types.Map
			diags = resp.State.GetAttribute(ctx, path.Root("marking_role_assignments"), &gotMap)
			if diags.HasError() {
				t.Fatalf("failed to read marking_role_assignments from upgraded state: %v", diags.Errors())
			}

			if len(tt.expectedRoles) == 0 {
				if !gotMap.IsNull() && len(gotMap.Elements()) != 0 {
					t.Errorf("expected empty or null map, got %d elements", len(gotMap.Elements()))
				}
				return
			}

			if len(gotMap.Elements()) != len(tt.expectedRoles) {
				t.Fatalf("map has %d roles, want %d", len(gotMap.Elements()), len(tt.expectedRoles))
			}

			for roleID, expectedPrincipals := range tt.expectedRoles {
				val, exists := gotMap.Elements()[roleID]
				if !exists {
					t.Errorf("missing role %q in upgraded map", roleID)
					continue
				}
				setVal, ok := val.(types.Set)
				if !ok {
					t.Errorf("role %q: expected types.Set, got %T", roleID, val)
					continue
				}

				var gotPrincipals []string
				diags := setVal.ElementsAs(ctx, &gotPrincipals, false)
				if diags.HasError() {
					t.Errorf("role %q: failed to extract principals: %v", roleID, diags.Errors())
					continue
				}

				sort.Strings(gotPrincipals)
				sort.Strings(expectedPrincipals)

				if len(gotPrincipals) != len(expectedPrincipals) {
					t.Errorf("role %q: got %d principals %v, want %d %v", roleID, len(gotPrincipals), gotPrincipals, len(expectedPrincipals), expectedPrincipals)
					continue
				}
				for i := range gotPrincipals {
					if gotPrincipals[i] != expectedPrincipals[i] {
						t.Errorf("role %q: principal[%d] = %q, want %q", roleID, i, gotPrincipals[i], expectedPrincipals[i])
					}
				}
			}
		})
	}
}
