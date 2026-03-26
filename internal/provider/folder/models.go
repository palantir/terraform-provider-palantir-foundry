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

package folder

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type folderResourceModel struct {
	RID             types.String `tfsdk:"rid"`
	DisplayName     types.String `tfsdk:"display_name"`
	ParentFolderRID types.String `tfsdk:"parent_folder_rid"`
	TrashStatus     types.String `tfsdk:"trash_status"`
	Path            types.String `tfsdk:"path"`
	CreatedBy       types.String `tfsdk:"created_by"`
	CreatedTime     types.String `tfsdk:"created_time"`
	UpdatedBy       types.String `tfsdk:"updated_by"`
	UpdatedTime     types.String `tfsdk:"updated_time"`
}
