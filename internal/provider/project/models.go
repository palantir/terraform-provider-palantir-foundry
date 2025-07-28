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
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type projectResourceModel struct {
	RID           types.String `tfsdk:"rid"`
	DisplayName   types.String `tfsdk:"display_name"`
	SpaceRID      types.String `tfsdk:"space_rid"`
	Description   types.String `tfsdk:"description"`
	Organizations types.Set    `tfsdk:"organizations"`
	ResourceRoles types.Set    `tfsdk:"resource_roles"`
	Markings      types.Set    `tfsdk:"markings"`
	TrashStatus   types.String `tfsdk:"trash_status"`
}

// requestBody contains the schema for request body
type responseBody struct {
	RID         string `json:"rid"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	SpaceRID    string `json:"spaceRid"`
	TrashStatus string `json:"trashStatus"`
}

type listOrganizationsResponseBody struct {
	Data []string `json:"data"`
}

type ResourceRolesResponse struct {
	Roles []struct {
		ResourceRolePrincipal struct {
			Type          string `json:"type"`
			PrincipalID   string `json:"principalId"`
			PrincipalType string `json:"principalType"`
		} `json:"resourceRolePrincipal"`
		RoleID string `json:"roleId"`
	} `json:"data"`
}

type listMarkingsResponseBody struct {
	Data []string `json:"data"`
}

type ResourceRole struct {
	ResourceRolePrincipal struct {
		Type          string `json:"type" tfsdk:"type"`
		PrincipalID   string `json:"principalId" tfsdk:"principal_id"`
		PrincipalType string `json:"principalType" tfsdk:"principal_type"`
	} `tfsdk:"resource_role_principal" json:"resourceRolePrincipal"`
	RoleID string `json:"roleId" tfsdk:"role_id"`
}
