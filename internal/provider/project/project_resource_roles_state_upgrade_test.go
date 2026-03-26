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
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

type resourceRolesV0Entry struct {
	principalType string
	principalID   *string
	principalKind *string // "USER" or "GROUP"
	roleID        string
}

var resourceRolePrincipalTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"type":           tftypes.String,
		"principal_id":   tftypes.String,
		"principal_type": tftypes.String,
	},
}

var resourceRolesV0EntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"resource_role_principal": resourceRolePrincipalTfType,
		"role_id":                 tftypes.String,
	},
}

var resourceRolesV0RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"project_rid":            tftypes.String,
		"project_resource_roles": tftypes.Set{ElementType: resourceRolesV0EntryTfType},
	},
}

var principalRoleEntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"groups": tftypes.Set{ElementType: tftypes.String},
		"users":  tftypes.Set{ElementType: tftypes.String},
	},
}

var resourceRolesV1RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"project_rid": tftypes.String,
		"principal_roles": tftypes.Map{
			ElementType: principalRoleEntryTfType,
		},
		"default_roles": tftypes.Set{ElementType: tftypes.String},
	},
}

func resourceRolesTestV1Schema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"project_rid": schema.StringAttribute{Required: true},
			"principal_roles": principalRolesMapSchema(
				"Map of Role ID to groups and users."),
			"default_roles": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func buildResourceRolesV0State(projectRid string, entries []resourceRolesV0Entry) tfsdk.State {
	roleValues := make([]tftypes.Value, len(entries))
	for i, e := range entries {
		principalIDVal := tftypes.NewValue(tftypes.String, nil)
		if e.principalID != nil {
			principalIDVal = tftypes.NewValue(tftypes.String, *e.principalID)
		}
		principalTypeVal := tftypes.NewValue(tftypes.String, nil)
		if e.principalKind != nil {
			principalTypeVal = tftypes.NewValue(tftypes.String, *e.principalKind)
		}
		principalObj := tftypes.NewValue(resourceRolePrincipalTfType, map[string]tftypes.Value{
			"type":           tftypes.NewValue(tftypes.String, e.principalType),
			"principal_id":   principalIDVal,
			"principal_type": principalTypeVal,
		})
		roleValues[i] = tftypes.NewValue(resourceRolesV0EntryTfType, map[string]tftypes.Value{
			"resource_role_principal": principalObj,
			"role_id":                 tftypes.NewValue(tftypes.String, e.roleID),
		})
	}

	raw := tftypes.NewValue(resourceRolesV0RootTfType, map[string]tftypes.Value{
		"project_rid":            tftypes.NewValue(tftypes.String, projectRid),
		"project_resource_roles": tftypes.NewValue(tftypes.Set{ElementType: resourceRolesV0EntryTfType}, roleValues),
	})

	return tfsdk.State{Schema: projectResourceRolesV0Schema(), Raw: raw}
}

func buildEmptyResourceRolesV1State() tfsdk.State {
	raw := tftypes.NewValue(resourceRolesV1RootTfType, map[string]tftypes.Value{
		"project_rid": tftypes.NewValue(tftypes.String, ""),
		"principal_roles": tftypes.NewValue(
			tftypes.Map{ElementType: principalRoleEntryTfType},
			map[string]tftypes.Value{},
		),
		"default_roles": tftypes.NewValue(
			tftypes.Set{ElementType: tftypes.String},
			[]tftypes.Value{},
		),
	})

	return tfsdk.State{Schema: resourceRolesTestV1Schema(), Raw: raw}
}

func strPtr(s string) *string {
	return &s
}

func TestProjectResourceRolesUpgradeStateV0ToV1(t *testing.T) {
	tests := []struct {
		name                 string
		projectRid           string
		v0Entries            []resourceRolesV0Entry
		expectedGroups       map[string][]string // role -> sorted group IDs
		expectedUsers        map[string][]string // role -> sorted user IDs
		expectedDefaultRoles []string
	}{
		{
			name:       "single principal role single user",
			projectRid: "ri.foundry.main.project.abc123",
			v0Entries: []resourceRolesV0Entry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-viewer"},
			},
			expectedGroups: map[string][]string{},
			expectedUsers: map[string][]string{
				"role-viewer": {"user-1"},
			},
			expectedDefaultRoles: []string{},
		},
		{
			name:       "single role mixed users and groups are separated",
			projectRid: "ri.foundry.main.project.abc123",
			v0Entries: []resourceRolesV0Entry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-editor"},
				{principalType: "principalWithId", principalID: strPtr("group-2"), principalKind: strPtr("GROUP"), roleID: "role-editor"},
				{principalType: "principalWithId", principalID: strPtr("user-3"), principalKind: strPtr("USER"), roleID: "role-editor"},
			},
			expectedGroups: map[string][]string{
				"role-editor": {"group-2"},
			},
			expectedUsers: map[string][]string{
				"role-editor": {"user-1", "user-3"},
			},
			expectedDefaultRoles: []string{},
		},
		{
			name:       "multiple principal roles with multiple principals",
			projectRid: "ri.foundry.main.project.abc123",
			v0Entries: []resourceRolesV0Entry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-viewer"},
				{principalType: "principalWithId", principalID: strPtr("group-2"), principalKind: strPtr("GROUP"), roleID: "role-editor"},
				{principalType: "principalWithId", principalID: strPtr("user-3"), principalKind: strPtr("USER"), roleID: "role-viewer"},
				{principalType: "principalWithId", principalID: strPtr("group-4"), principalKind: strPtr("GROUP"), roleID: "role-editor"},
				{principalType: "principalWithId", principalID: strPtr("user-5"), principalKind: strPtr("USER"), roleID: "role-owner"},
			},
			expectedGroups: map[string][]string{
				"role-editor": {"group-2", "group-4"},
			},
			expectedUsers: map[string][]string{
				"role-viewer": {"user-1", "user-3"},
				"role-owner":  {"user-5"},
			},
			expectedDefaultRoles: []string{},
		},
		{
			name:       "default roles only",
			projectRid: "ri.foundry.main.project.abc123",
			v0Entries: []resourceRolesV0Entry{
				{principalType: "everyone", roleID: "role-viewer"},
				{principalType: "everyone", roleID: "role-discoverer"},
			},
			expectedGroups:       map[string][]string{},
			expectedUsers:        map[string][]string{},
			expectedDefaultRoles: []string{"role-discoverer", "role-viewer"},
		},
		{
			name:       "mix of principal roles and default roles",
			projectRid: "ri.foundry.main.project.abc123",
			v0Entries: []resourceRolesV0Entry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-editor"},
				{principalType: "everyone", roleID: "role-viewer"},
				{principalType: "principalWithId", principalID: strPtr("group-2"), principalKind: strPtr("GROUP"), roleID: "role-editor"},
				{principalType: "everyone", roleID: "role-discoverer"},
			},
			expectedGroups: map[string][]string{
				"role-editor": {"group-2"},
			},
			expectedUsers: map[string][]string{
				"role-editor": {"user-1"},
			},
			expectedDefaultRoles: []string{"role-discoverer", "role-viewer"},
		},
		{
			name:                 "empty assignments produces empty map and empty set",
			projectRid:           "ri.foundry.main.project.abc123",
			v0Entries:            []resourceRolesV0Entry{},
			expectedGroups:       map[string][]string{},
			expectedUsers:        map[string][]string{},
			expectedDefaultRoles: []string{},
		},
		{
			name:       "project rid is preserved",
			projectRid: "ri.foundry.main.project.some-specific-id-value",
			v0Entries: []resourceRolesV0Entry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-viewer"},
			},
			expectedGroups: map[string][]string{},
			expectedUsers: map[string][]string{
				"role-viewer": {"user-1"},
			},
			expectedDefaultRoles: []string{},
		},
		{
			name:       "nil principal_type defaults to USER",
			projectRid: "ri.foundry.main.project.abc123",
			v0Entries: []resourceRolesV0Entry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: nil, roleID: "role-viewer"},
			},
			expectedGroups: map[string][]string{},
			expectedUsers: map[string][]string{
				"role-viewer": {"user-1"},
			},
			expectedDefaultRoles: []string{},
		},
	}

	r := &projectResourceRolesResource{}
	ctx := context.Background()
	upgraders := r.UpgradeState(ctx)
	upgrader, ok := upgraders[0]
	if !ok {
		t.Fatal("expected state upgrader for version 0")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqState := buildResourceRolesV0State(tt.projectRid, tt.v0Entries)
			respState := buildEmptyResourceRolesV1State()

			req := resource.UpgradeStateRequest{State: &reqState}
			resp := resource.UpgradeStateResponse{State: respState}

			upgrader.StateUpgrader(ctx, req, &resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics errors: %v", resp.Diagnostics.Errors())
			}

			// Verify project_rid is preserved
			var gotRid string
			diags := resp.State.GetAttribute(ctx, path.Root("project_rid"), &gotRid)
			if diags.HasError() {
				t.Fatalf("failed to read project_rid from upgraded state: %v", diags.Errors())
			}
			if gotRid != tt.projectRid {
				t.Errorf("project_rid: got %q, want %q", gotRid, tt.projectRid)
			}

			// Verify principal_roles map
			var gotMap types.Map
			diags = resp.State.GetAttribute(ctx, path.Root("principal_roles"), &gotMap)
			if diags.HasError() {
				t.Fatalf("failed to read principal_roles from upgraded state: %v", diags.Errors())
			}

			// Collect all expected roles
			allExpectedRoles := make(map[string]bool)
			for roleID := range tt.expectedGroups {
				allExpectedRoles[roleID] = true
			}
			for roleID := range tt.expectedUsers {
				allExpectedRoles[roleID] = true
			}

			if len(allExpectedRoles) == 0 {
				if !gotMap.IsNull() && len(gotMap.Elements()) != 0 {
					t.Errorf("expected empty or null principal_roles map, got %d elements", len(gotMap.Elements()))
				}
			} else {
				if len(gotMap.Elements()) != len(allExpectedRoles) {
					t.Fatalf("principal_roles has %d roles, want %d", len(gotMap.Elements()), len(allExpectedRoles))
				}

				for roleID := range allExpectedRoles {
					val, exists := gotMap.Elements()[roleID]
					if !exists {
						t.Errorf("missing role %q in upgraded principal_roles map", roleID)
						continue
					}
					obj, ok := val.(types.Object)
					if !ok {
						t.Errorf("role %q: expected types.Object, got %T", roleID, val)
						continue
					}

					// Check groups
					expectedGroupsList := tt.expectedGroups[roleID]
					groupsAttr := obj.Attributes()["groups"]
					if groupsAttr != nil && !groupsAttr.IsNull() {
						groupsSet := groupsAttr.(types.Set)
						var gotGroups []string
						diags := groupsSet.ElementsAs(ctx, &gotGroups, false)
						if diags.HasError() {
							t.Errorf("role %q: failed to extract groups: %v", roleID, diags.Errors())
							continue
						}
						sort.Strings(gotGroups)
						sort.Strings(expectedGroupsList)
						if len(gotGroups) != len(expectedGroupsList) {
							t.Errorf("role %q groups: got %v, want %v", roleID, gotGroups, expectedGroupsList)
						} else {
							for i := range gotGroups {
								if gotGroups[i] != expectedGroupsList[i] {
									t.Errorf("role %q groups[%d]: got %q, want %q", roleID, i, gotGroups[i], expectedGroupsList[i])
								}
							}
						}
					} else if len(expectedGroupsList) > 0 {
						t.Errorf("role %q: expected groups %v but got null/empty", roleID, expectedGroupsList)
					}

					// Check users
					expectedUsersList := tt.expectedUsers[roleID]
					usersAttr := obj.Attributes()["users"]
					if usersAttr != nil && !usersAttr.IsNull() {
						usersSet := usersAttr.(types.Set)
						var gotUsers []string
						diags := usersSet.ElementsAs(ctx, &gotUsers, false)
						if diags.HasError() {
							t.Errorf("role %q: failed to extract users: %v", roleID, diags.Errors())
							continue
						}
						sort.Strings(gotUsers)
						sort.Strings(expectedUsersList)
						if len(gotUsers) != len(expectedUsersList) {
							t.Errorf("role %q users: got %v, want %v", roleID, gotUsers, expectedUsersList)
						} else {
							for i := range gotUsers {
								if gotUsers[i] != expectedUsersList[i] {
									t.Errorf("role %q users[%d]: got %q, want %q", roleID, i, gotUsers[i], expectedUsersList[i])
								}
							}
						}
					} else if len(expectedUsersList) > 0 {
						t.Errorf("role %q: expected users %v but got null/empty", roleID, expectedUsersList)
					}
				}
			}

			// Verify default_roles set
			var gotDefaultRoles types.Set
			diags = resp.State.GetAttribute(ctx, path.Root("default_roles"), &gotDefaultRoles)
			if diags.HasError() {
				t.Fatalf("failed to read default_roles from upgraded state: %v", diags.Errors())
			}

			var gotDefaultStrings []string
			diags = gotDefaultRoles.ElementsAs(ctx, &gotDefaultStrings, false)
			if diags.HasError() {
				t.Fatalf("failed to convert default_roles: %v", diags.Errors())
			}

			sort.Strings(gotDefaultStrings)
			sort.Strings(tt.expectedDefaultRoles)

			if len(gotDefaultStrings) != len(tt.expectedDefaultRoles) {
				t.Errorf("default_roles: got %d items %v, want %d %v", len(gotDefaultStrings), gotDefaultStrings, len(tt.expectedDefaultRoles), tt.expectedDefaultRoles)
			} else {
				for i := range gotDefaultStrings {
					if gotDefaultStrings[i] != tt.expectedDefaultRoles[i] {
						t.Errorf("default_roles[%d] = %q, want %q", i, gotDefaultStrings[i], tt.expectedDefaultRoles[i])
					}
				}
			}
		})
	}
}
