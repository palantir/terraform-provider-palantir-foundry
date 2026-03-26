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

type categoryV0Assignment struct {
	role        string
	principalID string
}

var categoryV0RoleEntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"role":         tftypes.String,
		"principal_id": tftypes.String,
	},
}

var categoryV0InitialPermissionsTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"is_public":         tftypes.Bool,
		"organization_rids": tftypes.Set{ElementType: tftypes.String},
		"roles":             tftypes.Set{ElementType: categoryV0RoleEntryTfType},
	},
}

var categoryV0RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"id":                  tftypes.String,
		"name":                tftypes.String,
		"description":         tftypes.String,
		"category_type":       tftypes.String,
		"marking_type":        tftypes.String,
		"created_by":          tftypes.String,
		"created_time":        tftypes.String,
		"initial_permissions": categoryV0InitialPermissionsTfType,
	},
}

var categoryV1InitialPermissionsTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"is_public":         tftypes.Bool,
		"organization_rids": tftypes.Set{ElementType: tftypes.String},
		"roles":             tftypes.Map{ElementType: tftypes.Set{ElementType: tftypes.String}},
	},
}

var categoryV1RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"id":                  tftypes.String,
		"name":                tftypes.String,
		"description":         tftypes.String,
		"category_type":       tftypes.String,
		"marking_type":        tftypes.String,
		"created_by":          tftypes.String,
		"created_time":        tftypes.String,
		"initial_permissions": categoryV1InitialPermissionsTfType,
	},
}

func categoryTestV1Schema() schema.Schema {
	return schema.Schema{
		Version: 1,
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
					"roles": helper.RoleAssignmentMapSchema("Map of Role to set of Principal IDs."),
				},
			},
		},
	}
}

func buildCategoryV0State(id string, name string, orgRids []string, assignments []categoryV0Assignment) tfsdk.State {
	entries := make([]tftypes.Value, len(assignments))
	for i, a := range assignments {
		entries[i] = tftypes.NewValue(categoryV0RoleEntryTfType, map[string]tftypes.Value{
			"role":         tftypes.NewValue(tftypes.String, a.role),
			"principal_id": tftypes.NewValue(tftypes.String, a.principalID),
		})
	}

	orgRidValues := make([]tftypes.Value, len(orgRids))
	for i, rid := range orgRids {
		orgRidValues[i] = tftypes.NewValue(tftypes.String, rid)
	}

	raw := tftypes.NewValue(categoryV0RootTfType, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, id),
		"name":          tftypes.NewValue(tftypes.String, name),
		"description":   tftypes.NewValue(tftypes.String, "test description"),
		"category_type": tftypes.NewValue(tftypes.String, "CONJUNCTIVE"),
		"marking_type":  tftypes.NewValue(tftypes.String, "MANDATORY"),
		"created_by":    tftypes.NewValue(tftypes.String, "creator-id"),
		"created_time":  tftypes.NewValue(tftypes.String, "2025-01-01T00:00:00Z"),
		"initial_permissions": tftypes.NewValue(categoryV0InitialPermissionsTfType, map[string]tftypes.Value{
			"is_public":         tftypes.NewValue(tftypes.Bool, false),
			"organization_rids": tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, orgRidValues),
			"roles":             tftypes.NewValue(tftypes.Set{ElementType: categoryV0RoleEntryTfType}, entries),
		}),
	})

	return tfsdk.State{Schema: markingCategoryV0Schema(), Raw: raw}
}

func buildEmptyCategoryV1State() tfsdk.State {
	raw := tftypes.NewValue(categoryV1RootTfType, map[string]tftypes.Value{
		"id":            tftypes.NewValue(tftypes.String, ""),
		"name":          tftypes.NewValue(tftypes.String, ""),
		"description":   tftypes.NewValue(tftypes.String, ""),
		"category_type": tftypes.NewValue(tftypes.String, ""),
		"marking_type":  tftypes.NewValue(tftypes.String, ""),
		"created_by":    tftypes.NewValue(tftypes.String, ""),
		"created_time":  tftypes.NewValue(tftypes.String, ""),
		"initial_permissions": tftypes.NewValue(categoryV1InitialPermissionsTfType, map[string]tftypes.Value{
			"is_public":         tftypes.NewValue(tftypes.Bool, false),
			"organization_rids": tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, []tftypes.Value{}),
			"roles": tftypes.NewValue(
				tftypes.Map{ElementType: tftypes.Set{ElementType: tftypes.String}},
				map[string]tftypes.Value{},
			),
		}),
	})

	return tfsdk.State{Schema: categoryTestV1Schema(), Raw: raw}
}

func TestMarkingCategoryUpgradeStateV0ToV1(t *testing.T) {
	tests := []struct {
		name          string
		categoryID    string
		categoryName  string
		orgRids       []string
		v0Assignments []categoryV0Assignment
		expectedRoles map[string][]string // role -> sorted principal_ids
	}{
		{
			name:         "single role single principal",
			categoryID:   "cat-abc123",
			categoryName: "Test Category",
			orgRids:      []string{"ri.multipass..organization.org1"},
			v0Assignments: []categoryV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1"},
			},
		},
		{
			name:         "single role multiple principals are grouped",
			categoryID:   "cat-abc123",
			categoryName: "Test Category",
			orgRids:      []string{"ri.multipass..organization.org1"},
			v0Assignments: []categoryV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
				{role: "ADMINISTER", principalID: "user-2"},
				{role: "ADMINISTER", principalID: "user-3"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1", "user-2", "user-3"},
			},
		},
		{
			name:         "multiple roles with multiple principals",
			categoryID:   "cat-abc123",
			categoryName: "Test Category",
			orgRids:      []string{"ri.multipass..organization.org1"},
			v0Assignments: []categoryV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
				{role: "VIEW", principalID: "user-2"},
				{role: "ADMINISTER", principalID: "user-3"},
				{role: "VIEW", principalID: "user-4"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1", "user-3"},
				"VIEW":       {"user-2", "user-4"},
			},
		},
		{
			name:          "empty assignments produces empty map",
			categoryID:    "cat-abc123",
			categoryName:  "Test Category",
			orgRids:       []string{"ri.multipass..organization.org1"},
			v0Assignments: []categoryV0Assignment{},
			expectedRoles: map[string][]string{},
		},
		{
			name:         "category id is preserved",
			categoryID:   "cat-some-specific-id-value",
			categoryName: "Specific Category",
			orgRids:      []string{"ri.multipass..organization.org1"},
			v0Assignments: []categoryV0Assignment{
				{role: "ADMINISTER", principalID: "user-1"},
			},
			expectedRoles: map[string][]string{
				"ADMINISTER": {"user-1"},
			},
		},
	}

	r := &markingCategoryResource{}
	ctx := context.Background()
	upgraders := r.UpgradeState(ctx)
	upgrader, ok := upgraders[0]
	if !ok {
		t.Fatal("expected state upgrader for version 0")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqState := buildCategoryV0State(tt.categoryID, tt.categoryName, tt.orgRids, tt.v0Assignments)
			respState := buildEmptyCategoryV1State()

			req := resource.UpgradeStateRequest{State: &reqState}
			resp := resource.UpgradeStateResponse{State: respState}

			upgrader.StateUpgrader(ctx, req, &resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics errors: %v", resp.Diagnostics.Errors())
			}

			// Verify category id is preserved
			var gotID string
			diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID)
			if diags.HasError() {
				t.Fatalf("failed to read id from upgraded state: %v", diags.Errors())
			}
			if gotID != tt.categoryID {
				t.Errorf("id: got %q, want %q", gotID, tt.categoryID)
			}

			// Verify name is preserved
			var gotName string
			diags = resp.State.GetAttribute(ctx, path.Root("name"), &gotName)
			if diags.HasError() {
				t.Fatalf("failed to read name from upgraded state: %v", diags.Errors())
			}
			if gotName != tt.categoryName {
				t.Errorf("name: got %q, want %q", gotName, tt.categoryName)
			}

			// Verify role assignments map
			var gotMap types.Map
			diags = resp.State.GetAttribute(ctx, path.Root("initial_permissions").AtName("roles"), &gotMap)
			if diags.HasError() {
				t.Fatalf("failed to read roles from upgraded state: %v", diags.Errors())
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
