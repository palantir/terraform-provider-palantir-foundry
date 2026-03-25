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

package enrollment

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

type v0Assignment struct {
	roleID      string
	principalID string
}

var v0EntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"role_id":      tftypes.String,
		"principal_id": tftypes.String,
	},
}

var v0RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"enrollment_rid":              tftypes.String,
		"enrollment_role_assignments": tftypes.Set{ElementType: v0EntryTfType},
	},
}

var v1RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"enrollment_rid": tftypes.String,
		"enrollment_role_assignments": tftypes.Map{
			ElementType: tftypes.Set{ElementType: tftypes.String},
		},
	},
}

func testV1Schema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"enrollment_rid":              schema.StringAttribute{Required: true},
			"enrollment_role_assignments": helper.RoleAssignmentMapSchema("Map of Role ID to set of Principal IDs."),
		},
	}
}

func buildV0State(rid string, assignments []v0Assignment) tfsdk.State {
	entries := make([]tftypes.Value, len(assignments))
	for i, a := range assignments {
		entries[i] = tftypes.NewValue(v0EntryTfType, map[string]tftypes.Value{
			"role_id":      tftypes.NewValue(tftypes.String, a.roleID),
			"principal_id": tftypes.NewValue(tftypes.String, a.principalID),
		})
	}

	raw := tftypes.NewValue(v0RootTfType, map[string]tftypes.Value{
		"enrollment_rid":              tftypes.NewValue(tftypes.String, rid),
		"enrollment_role_assignments": tftypes.NewValue(tftypes.Set{ElementType: v0EntryTfType}, entries),
	})

	return tfsdk.State{Schema: V0Schema(), Raw: raw}
}

func buildEmptyV1State() tfsdk.State {
	raw := tftypes.NewValue(v1RootTfType, map[string]tftypes.Value{
		"enrollment_rid": tftypes.NewValue(tftypes.String, ""),
		"enrollment_role_assignments": tftypes.NewValue(
			tftypes.Map{ElementType: tftypes.Set{ElementType: tftypes.String}},
			map[string]tftypes.Value{},
		),
	})

	return tfsdk.State{Schema: testV1Schema(), Raw: raw}
}

func TestUpgradeStateV0ToV1(t *testing.T) {
	tests := []struct {
		name          string
		enrollmentRID string
		v0Assignments []v0Assignment
		expectedRoles map[string][]string // role_id -> sorted principal_ids
	}{
		{
			name:          "single role single principal",
			enrollmentRID: "ri.enrollment..abc123",
			v0Assignments: []v0Assignment{
				{roleID: "enrollment:administrator", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"enrollment:administrator": {"user-1"},
			},
		},
		{
			name:          "single role multiple principals are grouped",
			enrollmentRID: "ri.enrollment..abc123",
			v0Assignments: []v0Assignment{
				{roleID: "enrollment:administrator", principalID: "user-1"},
				{roleID: "enrollment:administrator", principalID: "user-2"},
				{roleID: "enrollment:administrator", principalID: "user-3"},
			},
			expectedRoles: map[string][]string{
				"enrollment:administrator": {"user-1", "user-2", "user-3"},
			},
		},
		{
			name:          "multiple roles with multiple principals",
			enrollmentRID: "ri.enrollment..abc123",
			v0Assignments: []v0Assignment{
				{roleID: "enrollment:administrator", principalID: "user-1"},
				{roleID: "enrollment:viewer", principalID: "user-2"},
				{roleID: "enrollment:administrator", principalID: "user-3"},
				{roleID: "enrollment:viewer", principalID: "user-4"},
				{roleID: "enrollment:editor", principalID: "user-5"},
			},
			expectedRoles: map[string][]string{
				"enrollment:administrator": {"user-1", "user-3"},
				"enrollment:viewer":        {"user-2", "user-4"},
				"enrollment:editor":        {"user-5"},
			},
		},
		{
			name:          "empty assignments produces empty map",
			enrollmentRID: "ri.enrollment..abc123",
			v0Assignments: []v0Assignment{},
			expectedRoles: map[string][]string{},
		},
		{
			name:          "enrollment rid is preserved",
			enrollmentRID: "ri.enrollment..some-specific-rid-value",
			v0Assignments: []v0Assignment{
				{roleID: "enrollment:administrator", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"enrollment:administrator": {"user-1"},
			},
		},
	}

	r := &enrollmentRoleAssignmentsResource{}
	ctx := context.Background()
	upgraders := r.UpgradeState(ctx)
	upgrader, ok := upgraders[0]
	if !ok {
		t.Fatal("expected state upgrader for version 0")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqState := buildV0State(tt.enrollmentRID, tt.v0Assignments)
			respState := buildEmptyV1State()

			req := resource.UpgradeStateRequest{State: &reqState}
			resp := resource.UpgradeStateResponse{State: respState}

			upgrader.StateUpgrader(ctx, req, &resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics errors: %v", resp.Diagnostics.Errors())
			}

			// Verify enrollment_rid is preserved
			var gotRID string
			diags := resp.State.GetAttribute(ctx, path.Root("enrollment_rid"), &gotRID)
			if diags.HasError() {
				t.Fatalf("failed to read enrollment_rid from upgraded state: %v", diags.Errors())
			}
			if gotRID != tt.enrollmentRID {
				t.Errorf("enrollment_rid: got %q, want %q", gotRID, tt.enrollmentRID)
			}

			// Verify role assignments map
			var gotMap types.Map
			diags = resp.State.GetAttribute(ctx, path.Root("enrollment_role_assignments"), &gotMap)
			if diags.HasError() {
				t.Fatalf("failed to read enrollment_role_assignments from upgraded state: %v", diags.Errors())
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
