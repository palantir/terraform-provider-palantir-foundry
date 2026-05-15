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
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// principalRoleEntry represents a single principal-to-role assignment with type information.
type principalRoleEntry struct {
	PrincipalID   string
	PrincipalType string // "USER" or "GROUP"
	RoleID        string
}

// principalRoleEntryObjectType is the attr.Type for a single entry in the principal_roles map.
var principalRoleEntryObjectType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"groups": types.SetType{ElemType: types.StringType},
		"users":  types.SetType{ElemType: types.StringType},
	},
}

// principalRolesMapSchema returns a schema.MapNestedAttribute for principal roles
// with the given description.
func principalRolesMapSchema(description string) schema.MapNestedAttribute {
	return schema.MapNestedAttribute{
		Description: description,
		Optional:    true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"groups": schema.SetAttribute{
					ElementType: types.StringType,
					Optional:    true,
					Description: "Set of Group IDs assigned to this role.",
				},
				"users": schema.SetAttribute{
					ElementType: types.StringType,
					Optional:    true,
					Description: "Set of User IDs assigned to this role.",
				},
			},
		},
	}
}

// flattenPrincipalRolesMap converts a types.Map (keys = role IDs, values = objects with
// groups and users sets) into a flat list of principalRoleEntry.
func flattenPrincipalRolesMap(ctx context.Context, m types.Map) ([]principalRoleEntry, error) {
	var result []principalRoleEntry
	for roleID, val := range m.Elements() {
		obj, ok := val.(types.Object)
		if !ok {
			return nil, fmt.Errorf("expected Object value for role %s, got %T", roleID, val)
		}

		groupsAttr := obj.Attributes()["groups"]
		if groupsAttr != nil && !groupsAttr.IsNull() && !groupsAttr.IsUnknown() {
			groupsSet, ok := groupsAttr.(types.Set)
			if !ok {
				return nil, fmt.Errorf("expected Set value for groups in role %s, got %T", roleID, groupsAttr)
			}
			var groups []string
			diags := groupsSet.ElementsAs(ctx, &groups, false)
			if diags.HasError() {
				return nil, fmt.Errorf("failed to extract groups for role %s", roleID)
			}
			for _, gid := range groups {
				result = append(result, principalRoleEntry{
					PrincipalID:   gid,
					PrincipalType: "GROUP",
					RoleID:        roleID,
				})
			}
		}

		usersAttr := obj.Attributes()["users"]
		if usersAttr != nil && !usersAttr.IsNull() && !usersAttr.IsUnknown() {
			usersSet, ok := usersAttr.(types.Set)
			if !ok {
				return nil, fmt.Errorf("expected Set value for users in role %s, got %T", roleID, usersAttr)
			}
			var users []string
			diags := usersSet.ElementsAs(ctx, &users, false)
			if diags.HasError() {
				return nil, fmt.Errorf("failed to extract users for role %s", roleID)
			}
			for _, uid := range users {
				result = append(result, principalRoleEntry{
					PrincipalID:   uid,
					PrincipalType: "USER",
					RoleID:        roleID,
				})
			}
		}
	}
	return result, nil
}

// buildPrincipalRolesMap groups flat principalRoleEntry values by role ID,
// splits by principal type, and builds a types.Map suitable for Terraform state.
// Returns a null map when entries is empty, and null sets for any role with
// zero groups or zero users, so the produced value matches the shape Terraform
// would derive from HCL where those fields are omitted.
func buildPrincipalRolesMap(entries []principalRoleEntry) (types.Map, error) {
	if len(entries) == 0 {
		return types.MapNull(principalRoleEntryObjectType), nil
	}

	type roleData struct {
		groups []string
		users  []string
	}

	grouped := make(map[string]*roleData)
	for _, entry := range entries {
		rd, ok := grouped[entry.RoleID]
		if !ok {
			rd = &roleData{}
			grouped[entry.RoleID] = rd
		}
		switch entry.PrincipalType {
		case "GROUP":
			rd.groups = append(rd.groups, entry.PrincipalID)
		case "USER":
			rd.users = append(rd.users, entry.PrincipalID)
		default:
			rd.users = append(rd.users, entry.PrincipalID)
		}
	}

	mapValues := make(map[string]attr.Value, len(grouped))
	for roleID, rd := range grouped {
		groupsSet := types.SetNull(types.StringType)
		if len(rd.groups) > 0 {
			groupValues := make([]attr.Value, len(rd.groups))
			for i, g := range rd.groups {
				groupValues[i] = types.StringValue(g)
			}
			var diags diag.Diagnostics
			groupsSet, diags = types.SetValue(types.StringType, groupValues)
			if diags.HasError() {
				return types.MapNull(principalRoleEntryObjectType), fmt.Errorf("failed to create groups set for role %s", roleID)
			}
		}

		usersSet := types.SetNull(types.StringType)
		if len(rd.users) > 0 {
			userValues := make([]attr.Value, len(rd.users))
			for i, u := range rd.users {
				userValues[i] = types.StringValue(u)
			}
			var diags diag.Diagnostics
			usersSet, diags = types.SetValue(types.StringType, userValues)
			if diags.HasError() {
				return types.MapNull(principalRoleEntryObjectType), fmt.Errorf("failed to create users set for role %s", roleID)
			}
		}

		objVal, diags := types.ObjectValue(
			map[string]attr.Type{
				"groups": types.SetType{ElemType: types.StringType},
				"users":  types.SetType{ElemType: types.StringType},
			},
			map[string]attr.Value{
				"groups": groupsSet,
				"users":  usersSet,
			},
		)
		if diags.HasError() {
			return types.MapNull(principalRoleEntryObjectType), fmt.Errorf("failed to create object for role %s", roleID)
		}
		mapValues[roleID] = objVal
	}

	mapValue, diags := types.MapValue(principalRoleEntryObjectType, mapValues)
	if diags.HasError() {
		return types.MapNull(principalRoleEntryObjectType), fmt.Errorf("failed to create principal roles map")
	}
	return mapValue, nil
}

// findPrincipalRolesDiff computes added and removed entries between two slices.
// Uses composite key of principalID|principalType|roleID.
func findPrincipalRolesDiff(oldSlice, newSlice []principalRoleEntry) (added, removed []principalRoleEntry) {
	key := func(e principalRoleEntry) string {
		return e.PrincipalID + "|" + e.PrincipalType + "|" + e.RoleID
	}

	oldMap := make(map[string]principalRoleEntry, len(oldSlice))
	newMap := make(map[string]principalRoleEntry, len(newSlice))

	for _, item := range oldSlice {
		oldMap[key(item)] = item
	}
	for _, item := range newSlice {
		newMap[key(item)] = item
	}

	newKeys := make([]string, 0, len(newMap))
	for k := range newMap {
		newKeys = append(newKeys, k)
	}
	sort.Strings(newKeys)

	oldKeys := make([]string, 0, len(oldMap))
	for k := range oldMap {
		oldKeys = append(oldKeys, k)
	}
	sort.Strings(oldKeys)

	for _, k := range newKeys {
		if _, exists := oldMap[k]; !exists {
			added = append(added, newMap[k])
		}
	}
	for _, k := range oldKeys {
		if _, exists := newMap[k]; !exists {
			removed = append(removed, oldMap[k])
		}
	}

	return added, removed
}
