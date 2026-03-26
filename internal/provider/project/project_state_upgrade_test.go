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

type projectV0RoleEntry struct {
	principalType string
	principalID   *string
	principalKind *string
	roleID        string
}

var projectV0RolePrincipalTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"type":           tftypes.String,
		"principal_id":   tftypes.String,
		"principal_type": tftypes.String,
	},
}

var projectV0RoleEntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"resource_role_principal": projectV0RolePrincipalTfType,
		"role_id":                 tftypes.String,
	},
}

var projectV0RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"rid":                    tftypes.String,
		"display_name":           tftypes.String,
		"space_rid":              tftypes.String,
		"description":            tftypes.String,
		"trash_status":           tftypes.String,
		"initial_resource_roles": tftypes.Set{ElementType: projectV0RoleEntryTfType},
		"initial_organizations":  tftypes.Set{ElementType: tftypes.String},
	},
}

var projectV1PrincipalRoleEntryTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"groups": tftypes.Set{ElementType: tftypes.String},
		"users":  tftypes.Set{ElementType: tftypes.String},
	},
}

var projectV1RootTfType = tftypes.Object{
	AttributeTypes: map[string]tftypes.Type{
		"rid":          tftypes.String,
		"display_name": tftypes.String,
		"space_rid":    tftypes.String,
		"description":  tftypes.String,
		"trash_status": tftypes.String,
		"initial_principal_roles": tftypes.Map{
			ElementType: projectV1PrincipalRoleEntryTfType,
		},
		"initial_organizations": tftypes.Set{ElementType: tftypes.String},
	},
}

func projectTestV1Schema() schema.Schema {
	return schema.Schema{
		Attributes: map[string]schema.Attribute{
			"rid":          schema.StringAttribute{Computed: true},
			"display_name": schema.StringAttribute{Required: true},
			"space_rid":    schema.StringAttribute{Required: true},
			"description":  schema.StringAttribute{Optional: true},
			"trash_status": schema.StringAttribute{Computed: true},
			"initial_principal_roles": principalRolesMapSchema(
				"Map of Role ID to groups and users."),
			"initial_organizations": schema.SetAttribute{
				Optional:    true,
				ElementType: types.StringType,
			},
		},
	}
}

func buildProjectV0State(rid, displayName, spaceRid, description, trashStatus string, roles []projectV0RoleEntry, orgs []string) tfsdk.State {
	roleValues := make([]tftypes.Value, len(roles))
	for i, e := range roles {
		principalIDVal := tftypes.NewValue(tftypes.String, nil)
		if e.principalID != nil {
			principalIDVal = tftypes.NewValue(tftypes.String, *e.principalID)
		}
		principalTypeVal := tftypes.NewValue(tftypes.String, nil)
		if e.principalKind != nil {
			principalTypeVal = tftypes.NewValue(tftypes.String, *e.principalKind)
		}
		principalObj := tftypes.NewValue(projectV0RolePrincipalTfType, map[string]tftypes.Value{
			"type":           tftypes.NewValue(tftypes.String, e.principalType),
			"principal_id":   principalIDVal,
			"principal_type": principalTypeVal,
		})
		roleValues[i] = tftypes.NewValue(projectV0RoleEntryTfType, map[string]tftypes.Value{
			"resource_role_principal": principalObj,
			"role_id":                 tftypes.NewValue(tftypes.String, e.roleID),
		})
	}

	orgValues := make([]tftypes.Value, len(orgs))
	for i, o := range orgs {
		orgValues[i] = tftypes.NewValue(tftypes.String, o)
	}

	raw := tftypes.NewValue(projectV0RootTfType, map[string]tftypes.Value{
		"rid":                    tftypes.NewValue(tftypes.String, rid),
		"display_name":           tftypes.NewValue(tftypes.String, displayName),
		"space_rid":              tftypes.NewValue(tftypes.String, spaceRid),
		"description":            tftypes.NewValue(tftypes.String, description),
		"trash_status":           tftypes.NewValue(tftypes.String, trashStatus),
		"initial_resource_roles": tftypes.NewValue(tftypes.Set{ElementType: projectV0RoleEntryTfType}, roleValues),
		"initial_organizations":  tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, orgValues),
	})

	return tfsdk.State{Schema: projectV0Schema(), Raw: raw}
}

func buildEmptyProjectV1State() tfsdk.State {
	raw := tftypes.NewValue(projectV1RootTfType, map[string]tftypes.Value{
		"rid":          tftypes.NewValue(tftypes.String, ""),
		"display_name": tftypes.NewValue(tftypes.String, ""),
		"space_rid":    tftypes.NewValue(tftypes.String, ""),
		"description":  tftypes.NewValue(tftypes.String, nil),
		"trash_status": tftypes.NewValue(tftypes.String, ""),
		"initial_principal_roles": tftypes.NewValue(
			tftypes.Map{ElementType: projectV1PrincipalRoleEntryTfType},
			map[string]tftypes.Value{},
		),
		"initial_organizations": tftypes.NewValue(
			tftypes.Set{ElementType: tftypes.String},
			[]tftypes.Value{},
		),
	})

	return tfsdk.State{Schema: projectTestV1Schema(), Raw: raw}
}

func TestProjectUpgradeStateV0ToV1(t *testing.T) {
	tests := []struct {
		name           string
		rid            string
		displayName    string
		spaceRid       string
		description    string
		trashStatus    string
		v0Roles        []projectV0RoleEntry
		orgs           []string
		expectedGroups map[string][]string // role -> sorted group IDs
		expectedUsers  map[string][]string // role -> sorted user IDs
	}{
		{
			name:           "no initial roles produces empty map",
			rid:            "ri.foundry.main.project.abc123",
			displayName:    "My Project",
			spaceRid:       "ri.foundry.main.space.xyz",
			description:    "A description",
			trashStatus:    "NOT_TRASHED",
			v0Roles:        []projectV0RoleEntry{},
			orgs:           []string{},
			expectedGroups: map[string][]string{},
			expectedUsers:  map[string][]string{},
		},
		{
			name:        "principal roles correctly separated into groups and users",
			rid:         "ri.foundry.main.project.abc123",
			displayName: "My Project",
			spaceRid:    "ri.foundry.main.space.xyz",
			description: "A description",
			trashStatus: "NOT_TRASHED",
			v0Roles: []projectV0RoleEntry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-viewer"},
				{principalType: "principalWithId", principalID: strPtr("group-2"), principalKind: strPtr("GROUP"), roleID: "role-viewer"},
				{principalType: "principalWithId", principalID: strPtr("user-3"), principalKind: strPtr("USER"), roleID: "role-editor"},
			},
			orgs: []string{},
			expectedGroups: map[string][]string{
				"role-viewer": {"group-2"},
			},
			expectedUsers: map[string][]string{
				"role-viewer": {"user-1"},
				"role-editor": {"user-3"},
			},
		},
		{
			name:        "mixed principal and everyone roles - only principal preserved",
			rid:         "ri.foundry.main.project.abc123",
			displayName: "My Project",
			spaceRid:    "ri.foundry.main.space.xyz",
			description: "A description",
			trashStatus: "NOT_TRASHED",
			v0Roles: []projectV0RoleEntry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-editor"},
				{principalType: "everyone", roleID: "role-viewer"},
				{principalType: "everyone", roleID: "role-discoverer"},
			},
			orgs:           []string{},
			expectedGroups: map[string][]string{},
			expectedUsers: map[string][]string{
				"role-editor": {"user-1"},
			},
		},
		{
			name:        "all existing fields are preserved",
			rid:         "ri.foundry.main.project.specific-rid",
			displayName: "Specific Project Name",
			spaceRid:    "ri.foundry.main.space.specific-space",
			description: "Specific description",
			trashStatus: "DIRECTLY_TRASHED",
			v0Roles: []projectV0RoleEntry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: strPtr("USER"), roleID: "role-viewer"},
			},
			orgs:           []string{"org-1", "org-2"},
			expectedGroups: map[string][]string{},
			expectedUsers: map[string][]string{
				"role-viewer": {"user-1"},
			},
		},
		{
			name:        "nil principal_type defaults to USER",
			rid:         "ri.foundry.main.project.abc123",
			displayName: "My Project",
			spaceRid:    "ri.foundry.main.space.xyz",
			description: "A description",
			trashStatus: "NOT_TRASHED",
			v0Roles: []projectV0RoleEntry{
				{principalType: "principalWithId", principalID: strPtr("user-1"), principalKind: nil, roleID: "role-viewer"},
			},
			orgs:           []string{},
			expectedGroups: map[string][]string{},
			expectedUsers: map[string][]string{
				"role-viewer": {"user-1"},
			},
		},
	}

	r := &projectResource{}
	ctx := context.Background()
	upgraders := r.UpgradeState(ctx)
	upgrader, ok := upgraders[0]
	if !ok {
		t.Fatal("expected state upgrader for version 0")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqState := buildProjectV0State(tt.rid, tt.displayName, tt.spaceRid, tt.description, tt.trashStatus, tt.v0Roles, tt.orgs)
			respState := buildEmptyProjectV1State()

			req := resource.UpgradeStateRequest{State: &reqState}
			resp := resource.UpgradeStateResponse{State: respState}

			upgrader.StateUpgrader(ctx, req, &resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected diagnostics errors: %v", resp.Diagnostics.Errors())
			}

			// Verify scalar fields
			var gotRid string
			diags := resp.State.GetAttribute(ctx, path.Root("rid"), &gotRid)
			if diags.HasError() {
				t.Fatalf("failed to read rid: %v", diags.Errors())
			}
			if gotRid != tt.rid {
				t.Errorf("rid: got %q, want %q", gotRid, tt.rid)
			}

			var gotDisplayName string
			diags = resp.State.GetAttribute(ctx, path.Root("display_name"), &gotDisplayName)
			if diags.HasError() {
				t.Fatalf("failed to read display_name: %v", diags.Errors())
			}
			if gotDisplayName != tt.displayName {
				t.Errorf("display_name: got %q, want %q", gotDisplayName, tt.displayName)
			}

			var gotSpaceRid string
			diags = resp.State.GetAttribute(ctx, path.Root("space_rid"), &gotSpaceRid)
			if diags.HasError() {
				t.Fatalf("failed to read space_rid: %v", diags.Errors())
			}
			if gotSpaceRid != tt.spaceRid {
				t.Errorf("space_rid: got %q, want %q", gotSpaceRid, tt.spaceRid)
			}

			var gotTrashStatus string
			diags = resp.State.GetAttribute(ctx, path.Root("trash_status"), &gotTrashStatus)
			if diags.HasError() {
				t.Fatalf("failed to read trash_status: %v", diags.Errors())
			}
			if gotTrashStatus != tt.trashStatus {
				t.Errorf("trash_status: got %q, want %q", gotTrashStatus, tt.trashStatus)
			}

			// Verify initial_principal_roles map
			var gotMap types.Map
			diags = resp.State.GetAttribute(ctx, path.Root("initial_principal_roles"), &gotMap)
			if diags.HasError() {
				t.Fatalf("failed to read initial_principal_roles: %v", diags.Errors())
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
					t.Errorf("expected empty or null map, got %d elements", len(gotMap.Elements()))
				}
			} else {
				if len(gotMap.Elements()) != len(allExpectedRoles) {
					t.Fatalf("map has %d roles, want %d", len(gotMap.Elements()), len(allExpectedRoles))
				}

				for roleID := range allExpectedRoles {
					val, exists := gotMap.Elements()[roleID]
					if !exists {
						t.Errorf("missing role %q in upgraded map", roleID)
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

			// Verify organizations are preserved
			if len(tt.orgs) > 0 {
				var gotOrgs types.Set
				diags = resp.State.GetAttribute(ctx, path.Root("initial_organizations"), &gotOrgs)
				if diags.HasError() {
					t.Fatalf("failed to read initial_organizations: %v", diags.Errors())
				}
				var gotOrgStrings []string
				diags = gotOrgs.ElementsAs(ctx, &gotOrgStrings, false)
				if diags.HasError() {
					t.Fatalf("failed to convert initial_organizations: %v", diags.Errors())
				}
				sort.Strings(gotOrgStrings)
				sort.Strings(tt.orgs)
				if len(gotOrgStrings) != len(tt.orgs) {
					t.Errorf("organizations: got %v, want %v", gotOrgStrings, tt.orgs)
				}
			}
		})
	}
}
