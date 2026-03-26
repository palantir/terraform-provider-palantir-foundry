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

package helper

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// RoleAssignmentEntry represents a single role-to-principal assignment.
// This is a generic representation used for conversions between the Terraform
// map schema and the flat API format.
type RoleAssignmentEntry struct {
	RoleIdentifier string
	PrincipalID    string
}

// RoleAssignmentMapElementType returns the element type for a role assignment map attribute.
var RoleAssignmentMapElementType = types.SetType{ElemType: types.StringType}

// RoleAssignmentMapSchema returns a schema.MapAttribute for role assignments
// with the given description. The schema is map<string, set<string>> where
// keys are role identifiers and values are sets of principal IDs.
func RoleAssignmentMapSchema(description string) schema.MapAttribute {
	return schema.MapAttribute{
		Description: description,
		Required:    true,
		ElementType: RoleAssignmentMapElementType,
	}
}

// FlattenRoleAssignmentMap converts a types.Map (keys = role identifiers,
// values = types.Set of principal ID strings) into a flat list of
// RoleAssignmentEntry. Returns nil for null or unknown maps.
func FlattenRoleAssignmentMap(ctx context.Context, m types.Map) ([]RoleAssignmentEntry, error) {
	var result []RoleAssignmentEntry
	for roleID, val := range m.Elements() {
		setVal, ok := val.(types.Set)
		if !ok {
			return nil, fmt.Errorf("expected Set value for role %s, got %T", roleID, val)
		}
		var principals []string
		diags := setVal.ElementsAs(ctx, &principals, false)
		if diags.HasError() {
			return nil, fmt.Errorf("failed to extract principals for role %s", roleID)
		}
		for _, principalID := range principals {
			result = append(result, RoleAssignmentEntry{
				RoleIdentifier: roleID,
				PrincipalID:    principalID,
			})
		}
	}
	return result, nil
}

// BuildRoleAssignmentMap groups flat RoleAssignmentEntry values by role
// identifier and builds a types.Map of types.Set values suitable for
// setting Terraform state.
func BuildRoleAssignmentMap(ctx context.Context, entries []RoleAssignmentEntry) (types.Map, error) {
	grouped := make(map[string][]string)
	for _, entry := range entries {
		grouped[entry.RoleIdentifier] = append(grouped[entry.RoleIdentifier], entry.PrincipalID)
	}

	mapValues := make(map[string]attr.Value, len(grouped))
	for roleID, principals := range grouped {
		principalValues := make([]attr.Value, len(principals))
		for i, p := range principals {
			principalValues[i] = types.StringValue(p)
		}
		setValue, diags := types.SetValue(types.StringType, principalValues)
		if diags.HasError() {
			return types.MapNull(RoleAssignmentMapElementType), fmt.Errorf("failed to create set for role %s", roleID)
		}
		mapValues[roleID] = setValue
	}

	mapValue, diags := types.MapValue(RoleAssignmentMapElementType, mapValues)
	if diags.HasError() {
		return types.MapNull(RoleAssignmentMapElementType), fmt.Errorf("failed to create role assignment map")
	}
	return mapValue, nil
}

// FindRoleAssignmentsDiff computes the added and removed entries between
// two slices of RoleAssignmentEntry. Uses composite key of principalID|roleIdentifier.
func FindRoleAssignmentsDiff(oldSlice, newSlice []RoleAssignmentEntry) (added, removed []RoleAssignmentEntry) {
	oldMap := make(map[string]RoleAssignmentEntry, len(oldSlice))
	newMap := make(map[string]RoleAssignmentEntry, len(newSlice))

	for _, item := range oldSlice {
		key := item.PrincipalID + "|" + item.RoleIdentifier
		oldMap[key] = item
	}
	for _, item := range newSlice {
		key := item.PrincipalID + "|" + item.RoleIdentifier
		newMap[key] = item
	}

	// Collect keys and sort for deterministic output
	newKeys := make([]string, 0, len(newMap))
	for key := range newMap {
		newKeys = append(newKeys, key)
	}
	sort.Strings(newKeys)

	oldKeys := make([]string, 0, len(oldMap))
	for key := range oldMap {
		oldKeys = append(oldKeys, key)
	}
	sort.Strings(oldKeys)

	for _, key := range newKeys {
		if _, exists := oldMap[key]; !exists {
			added = append(added, newMap[key])
		}
	}

	for _, key := range oldKeys {
		if _, exists := newMap[key]; !exists {
			removed = append(removed, oldMap[key])
		}
	}

	return added, removed
}

// MapHasRole checks if a role assignment map contains at least one
// principal for the given role identifier.
func MapHasRole(m types.Map, roleID string) bool {
	if m.IsNull() || m.IsUnknown() {
		return false
	}
	val, exists := m.Elements()[roleID]
	if !exists {
		return false
	}
	setVal, ok := val.(types.Set)
	if !ok {
		return false
	}
	return len(setVal.Elements()) > 0
}
