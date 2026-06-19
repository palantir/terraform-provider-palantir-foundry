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

package usage_account

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type usageAccountResourceModel struct {
	RID           types.String `tfsdk:"rid"`
	EnrollmentRID types.String `tfsdk:"enrollment_rid"`
	DisplayName   types.String `tfsdk:"display_name"`
	Description   types.String `tfsdk:"description"`
	Internal      types.Bool   `tfsdk:"internal"`
}

// responseBody contains the schema for response body
type responseBody struct {
	RID           string `json:"rid"`
	EnrollmentRID string `json:"enrollmentRid"`
	DisplayName   string `json:"displayName"`
	Description   string `json:"description"`
	Internal      bool   `json:"internal"`
}
