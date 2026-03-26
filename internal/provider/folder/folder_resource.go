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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	v2 "github.com/palantir/terraform-provider-palantir-foundry/gateway-client/v2"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/constants"
	providerError "github.com/palantir/terraform-provider-palantir-foundry/internal/provider/errors"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/helper"
	"github.com/palantir/terraform-provider-palantir-foundry/internal/provider/shared"
)

func applyFolderToState(folder v2.FilesystemFolder, state *folderResourceModel) {
	state.RID = types.StringValue(folder.Rid)
	state.DisplayName = types.StringValue(folder.DisplayName)
	state.ParentFolderRID = types.StringValue(folder.ParentFolderRid)
	state.TrashStatus = types.StringValue(string(folder.TrashStatus))
	state.Path = types.StringValue(folder.Path)
	state.CreatedBy = types.StringValue(folder.CreatedBy.String())
	state.CreatedTime = types.StringValue(folder.CreatedTime.Format(time.RFC3339))
	state.UpdatedBy = types.StringValue(folder.UpdatedBy.String())
	state.UpdatedTime = types.StringValue(folder.UpdatedTime.Format(time.RFC3339))
}

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource              = &folderResource{}
	_ resource.ResourceWithConfigure = &folderResource{}
)

// NewFolderResource is a helper function to simplify provider implementation.
func NewFolderResource() resource.Resource {
	return &folderResource{}
}

// folderResource is the resource implementation.
type folderResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
	deleteMode        string
}

// Configure adds the provider data to the resource.
func (r *folderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	providerData, ok := req.ProviderData.(*shared.FoundryProviderData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected shared.FoundryProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = providerData.Client
	r.deletionsDisabled = providerData.Flags.DeletionsDisabled
	r.deleteMode = providerData.Flags.DeleteMode
}

// Metadata returns the resource type name.
func (r *folderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_folder"
}

// Schema defines the schema for the resource.
func (r *folderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry Folder.",
		Attributes: map[string]schema.Attribute{
			"rid": schema.StringAttribute{
				Description: "RID of the Folder.",
				Computed:    true,
			},
			"display_name": schema.StringAttribute{
				Description: "Display name of the Folder.",
				Required:    true,
			},
			"parent_folder_rid": schema.StringAttribute{
				Description: "RID of the parent Folder. For resources, this should be the RID of a project or another folder. " +
					"For datasources of projects or spaces, this may be a space RID.",
				Required: true,
			},
			"trash_status": schema.StringAttribute{
				Description: "Current trash status of the Folder.",
				Computed:    true,
			},
			"path": schema.StringAttribute{
				Description: "The full path to the Folder.",
				Computed:    true,
			},
			"created_by": schema.StringAttribute{
				Description: "The ID of the user who created the Folder.",
				Computed:    true,
			},
			"created_time": schema.StringAttribute{
				Description: "The time at which the Folder was created.",
				Computed:    true,
			},
			"updated_by": schema.StringAttribute{
				Description: "The ID of the user who last updated the Folder.",
				Computed:    true,
			},
			"updated_time": schema.StringAttribute{
				Description: "The time at which the Folder was most recently updated.",
				Computed:    true,
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *folderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan folderResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	previewMode := constants.PreviewMode
	filesystemCreateFolderParams := v2.FilesystemCreateFolderParams{Preview: &previewMode}

	httpResp, err := r.client.FilesystemCreateFolder(ctx,
		&filesystemCreateFolderParams,
		v2.FilesystemCreateFolderJSONRequestBody{
			DisplayName:     plan.DisplayName.ValueString(),
			ParentFolderRid: plan.ParentFolderRID.ValueString(),
		})

	tflog.Debug(ctx, fmt.Sprintf("FilesystemCreateFolder response: %+v", httpResp))

	if err != nil {
		resp.Diagnostics.AddError("FilesystemCreateFolder request failed", err.Error())
		return
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemCreateFolder response", err.Error())
			return
		}
		resp.Diagnostics.AddError("Response from FilesystemCreateFolder was unsuccessful: ", returnString)
		return
	}

	//if success - take id from the response and update the state
	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from FilesystemCreateFolder", err.Error())
		return
	}
	var folder v2.FilesystemFolder
	if err := json.Unmarshal(bodyBytes, &folder); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return
	}

	//CREATE - do not save state if id is not saved
	if folder.Rid == "" {
		tflog.Error(ctx, "RID was not populated in response, "+
			"so Terraform best practice is NOT to update state as resource likely was not properly created")
		resp.Diagnostics.AddError("ID returned as empty",
			"ID was not populated in response, "+
				"so Terraform best practice is NOT to update state as resource likely was not properly created")
		return
	}

	applyFolderToState(folder, &plan)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Read refreshes the Terraform state with the latest data.
func (r *folderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state folderResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	previewMode := constants.PreviewMode
	filesystemGetFolderParams := v2.FilesystemGetFolderParams{Preview: &previewMode}
	httpResp, err := r.client.FilesystemGetFolder(ctx, state.RID.ValueString(), &filesystemGetFolderParams)

	if err != nil {
		resp.Diagnostics.AddError("FilesystemGetFolder request failed", err.Error())
		return
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusNotFound {
			resp.Diagnostics.Append(providerError.ResourceNotFoundWarning(state.RID.ValueString(), "Folder"))
			resp.State.RemoveResource(ctx)
			return
		}
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemGetFolder response", err.Error())
			return
		}
		resp.Diagnostics.AddError("Response from FilesystemGetFolder was unsuccessful: ", returnString)
		return
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from FilesystemGetFolder", err.Error())
		return
	}

	var folder v2.FilesystemFolder
	if err := json.Unmarshal(bodyBytes, &folder); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return
	}

	applyFolderToState(folder, &state)

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *folderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan folderResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state folderResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	previewMode := constants.PreviewMode
	filesystemReplaceFolderParams := v2.FilesystemReplaceFolderParams{Preview: &previewMode}

	httpResp, err := r.client.FilesystemReplaceFolder(ctx, state.RID.ValueString(), &filesystemReplaceFolderParams, v2.FilesystemReplaceFolderJSONRequestBody{
		DisplayName:     plan.DisplayName.ValueString(),
		ParentFolderRid: plan.ParentFolderRID.ValueString(),
	})

	if err != nil {
		resp.Diagnostics.AddError("FilesystemReplaceFolder request failed", err.Error())
		return
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemReplaceFolder response", err.Error())
			return
		}
		resp.Diagnostics.AddError("Response from FilesystemReplaceFolder was unsuccessful: ", returnString)
		return
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from FilesystemReplaceFolder", err.Error())
		return
	}

	var folder v2.FilesystemFolder
	if err := json.Unmarshal(bodyBytes, &folder); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return
	}

	applyFolderToState(folder, &state)

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *folderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state folderResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If deletions are disabled, do not delete the remote folder but remove the resource from state.
	if r.deletionsDisabled {
		resp.Diagnostics.AddWarning("Tried to perform a deletion when the deletions_disabled flag was set to true.",
			fmt.Sprintf("Remote folder with name %s and rid %s will not be deleted, but this resource will be removed from state.", state.DisplayName.ValueString(), state.RID.ValueString()))
		return
	}

	if r.deleteMode == shared.DeleteModeTrash {
		if state.TrashStatus.ValueString() == string(v2.NOTTRASHED) {
			err := r.DeleteResource(ctx, resp, &state)
			if err != nil {
				resp.Diagnostics.AddError("Error deleting the Folder", err.Error())
			}
		}
		return
	}

	// PERMANENTLY_DELETE mode: call permanent delete directly, skipping the trash endpoint.
	err := r.PermanentlyDeleteResource(ctx, resp, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error permanently deleting the folder resource", err.Error())
	}
}

func (r *folderResource) DeleteResource(ctx context.Context, resp *resource.DeleteResponse, state *folderResourceModel) error {
	previewMode := constants.PreviewMode
	filesystemDeleteResourceParams := v2.FilesystemDeleteResourceParams{Preview: &previewMode}
	httpResp, err := r.client.FilesystemDeleteResource(ctx, state.RID.ValueString(), &filesystemDeleteResourceParams)

	if err != nil {
		resp.Diagnostics.AddError("FilesystemDeleteResource request failed", err.Error())
		return fmt.Errorf("FilesystemDeleteResource request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemDeleteResource response", err.Error())
			return fmt.Errorf("failed to format error logging from FilesystemDeleteResource response: %w", err)
		}
		resp.Diagnostics.AddError("Response from FilesystemDeleteResource was unsuccessful: ", returnString)
		return fmt.Errorf("response from FilesystemDeleteResource was unsuccessful: %s", returnString)
	}
	state.TrashStatus = types.StringValue(string(v2.DIRECTLYTRASHED))
	return nil
}

func (r *folderResource) PermanentlyDeleteResource(ctx context.Context, resp *resource.DeleteResponse, state *folderResourceModel) error {
	previewMode := constants.PreviewMode
	filesystemPermanentlyDeleteResourceParams := v2.FilesystemPermanentlyDeleteResourceParams{Preview: &previewMode}
	httpResp, err := r.client.FilesystemPermanentlyDeleteResource(ctx, state.RID.ValueString(), &filesystemPermanentlyDeleteResourceParams)

	if err != nil {
		resp.Diagnostics.AddError("FilesystemPermanentlyDeleteResource request failed", err.Error())
		return fmt.Errorf("FilesystemPermanentlyDeleteResource request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemPermanentlyDeleteResource response", err.Error())
			return fmt.Errorf("failed to format error logging from FilesystemPermanentlyDeleteResource response: %w", err)
		}
		resp.Diagnostics.AddError("Response from FilesystemPermanentlyDeleteResource was unsuccessful: ", returnString)
		return fmt.Errorf("response from FilesystemPermanentlyDeleteResource was unsuccessful: %s", returnString)
	}
	return nil
}

// ImportState imports an existing folder into Terraform state.
func (r *folderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	folderID := req.ID

	if folderID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the folder RID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing folder with RID %s", folderID))

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rid"), folderID)...)
}
