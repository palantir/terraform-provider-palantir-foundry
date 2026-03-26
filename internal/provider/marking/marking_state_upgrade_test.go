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

type markingResV0Assignment struct {
	role        string
	principalID string
}

var markingResV0EntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"role":         tftypes.String,
		"principal_id": tftypes.String,
	},
}

var markingResV0RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"id":                       tftypes.String,
		"category_id":              tftypes.String,
		"name":                     tftypes.String,
		"description":              tftypes.String,
		"initial_role_assignments": tftypes.Set{ElementType: markingResV0EntryTfType},
	},
}

var markingResV1RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"id":          tftypes.String,
		"category_id": tftypes.String,
		"name":        tftypes.String,
		"description": tftypes.String,
		"initial_role_assignments": tftypes.Map{
			ElementType: tftypes.Set{ElementType: tftypes.String},
		},
	},
}

func markingResTestV1Schema() schema.Schema {
	initialRoleAssignmentsSchema := helper.RoleAssignmentMapSchema("Map of Role to set of Principal IDs.")

	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"id":                       schema.StringAttribute{Computed: true},
			"category_id":              schema.StringAttribute{Required: true},
			"name":                     schema.StringAttribute{Required: true},
			"description":              schema.StringAttribute{Optional: true},
			"initial_role_assignments": initialRoleAssignmentsSchema,
		},
	}
}

func buildMarkingResV0State(id string, categoryID string, name string, description string, assignments []markingResV0Assignment) tfsdk.State {
	entries := make([]tftypes.Value, len(assignments))
	for i, a := range assignments {
		entries[i] = tftypes.NewValue(markingResV0EntryTfType, map[string]tftypes.Value{
			"role":         tftypes.NewValue(tftypes.String, a.role),
			"principal_id": tftypes.NewValue(tftypes.String, a.principalID),
		})
	}

	raw := tftypes.NewValue(markingResV0RootTfType, map[string]tftypes.Value{
		"id":                       tftypes.NewValue(tftypes.String, id),
		"category_id":              tftypes.NewValue(tftypes.String, categoryID),
		"name":                     tftypes.NewValue(tftypes.String, name),
		"description":              tftypes.NewValue(tftypes.String, description),
		"initial_role_assignments": tftypes.NewValue(tftypes.Set{ElementType: markingResV0EntryTfType}, entries),
	})

	return tfsdk.State{Schema: markingV0Schema(), Raw: raw}
}

func buildEmptyMarkingResV1State() tfsdk.State {
	raw := tftypes.NewValue(markingResV1RootTfType, map[string]tftypes.Value{
		"id":          tftypes.NewValue(tftypes.String, ""),
		"category_id": tftypes.NewValue(tftypes.String, ""),
		"name":        tftypes.NewValue(tftypes.String, ""),
		"description": tftypes.NewValue(tftypes.String, ""),
		"initial_role_assignments": tftypes.NewValue(
			tftypes.Map{ElementType: tftypes.Set{ElementType: tftypes.String}},
			map[string]tftypes.Value{},
		),
	})

	return tfsdk.State{Schema: markingResTestV1Schema(), Raw: raw}
}

func TestMarkingResourceUpgradeStateV0ToV1(t *testing.T) {
	tests := []struct {
		name          string
		markingID     string
		categoryID    string
		markingName   string
		description   string
		v0Assignments []markingResV0Assignment
		expectedRoles map[string][]string // role -> sorted principal_ids
	}{
		{
			name:        "single role single principal",
			markingID:   "marking-abc123",
			categoryID:  "cat-1",
			markingName: "Test Marking",
			description: "A test",
			v0Assignments: []markingResV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1"},
			},
		},
		{
			name:        "single role multiple principals are grouped",
			markingID:   "marking-abc123",
			categoryID:  "cat-1",
			markingName: "Test Marking",
			description: "A test",
			v0Assignments: []markingResV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
				{role: "ADMINISTER", principalID: "user-2"},
				{role: "ADMINISTER", principalID: "user-3"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1", "user-2", "user-3"},
			},
		},
		{
			name:        "multiple roles with multiple principals",
			markingID:   "marking-abc123",
			categoryID:  "cat-1",
			markingName: "Test Marking",
			description: "A test",
			v0Assignments: []markingResV0Assignment{
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
			categoryID:    "cat-1",
			markingName:   "Test Marking",
			description:   "A test",
			v0Assignments: []markingResV0Assignment{},
			expectedRoles: map[string][]string{},
		},
		{
			name:        "marking id is preserved",
			markingID:   "marking-some-specific-id",
			categoryID:  "cat-specific",
			markingName: "Specific Marking",
			description: "Specific description",
			v0Assignments: []markingResV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1"},
			},
		},
	}

	r := &markingResource{}
	ctx := context.Background()
	upgraders := r.UpgradeState(ctx)
	upgrader, ok := upgraders[0]
	if !ok {
		t.Fatal("expected state upgrader for version 0")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqState := buildMarkingResV0State(tt.markingID, tt.categoryID, tt.markingName, tt.description, tt.v0Assignments)
			respState := buildEmptyMarkingResV1State()

			req := resource.UpgradeStateRequest{State: &reqState}
			resp := resource.UpgradeStateResponse{State: respState}

			upgrader.StateUpgrader(ctx, req, &resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics errors: %v", resp.Diagnostics.Errors())
			}

			// Verify id is preserved
			var gotID string
			diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID)
			if diags.HasError() {
				t.Fatalf("failed to read id from upgraded state: %v", diags.Errors())
			}
			if gotID != tt.markingID {
				t.Errorf("id: got %q, want %q", gotID, tt.markingID)
			}

			// Verify category_id is preserved
			var gotCategoryID string
			diags = resp.State.GetAttribute(ctx, path.Root("category_id"), &gotCategoryID)
			if diags.HasError() {
				t.Fatalf("failed to read category_id from upgraded state: %v", diags.Errors())
			}
			if gotCategoryID != tt.categoryID {
				t.Errorf("category_id: got %q, want %q", gotCategoryID, tt.categoryID)
			}

			// Verify role assignments map
			var gotMap types.Map
			diags = resp.State.GetAttribute(ctx, path.Root("initial_role_assignments"), &gotMap)
			if diags.HasError() {
				t.Fatalf("failed to read initial_role_assignments from upgraded state: %v", diags.Errors())
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
