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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

// Ensure the implementation satisfies the expected interfaces
var (
	_ resource.Resource              = &projectResource{}
	_ resource.ResourceWithConfigure = &projectResource{}
)

// NewProjectResource is a helper function to simplify provider implementation.
func NewProjectResource() resource.Resource {
	return &projectResource{}
}

// projectResource is the resource implementation.
type projectResource struct {
	client            *v2.ClientWithResponses
	deletionsDisabled bool
}

// Configure adds the provider data to the resource.
func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
}

// Metadata returns the resource type name.
func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

// Schema defines the schema for the resource.
func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Foundry project.",
		Attributes: map[string]schema.Attribute{
			"rid": schema.StringAttribute{
				Description: "RID of the Project.",
				Computed:    true,
			},
			"display_name": schema.StringAttribute{
				Description: "Display name of the Project.",
				Required:    true,
			},
			"space_rid": schema.StringAttribute{
				Description: "RID of the Space that this Project belongs to.",
				Required:    true,
			},
			"description": schema.StringAttribute{
				Description: "Description of the Project.",
				Optional:    true,
			},
			"organizations": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "List of Organizations applied to this Project.",
				Required:    true,
			},
			"resource_roles": schema.SetAttribute{
				Description: "Set of Role Grants applied to this Project.",
				Optional:    true,
				ElementType: types.ObjectType{
					AttrTypes: map[string]attr.Type{
						"resource_role_principal": types.ObjectType{
							AttrTypes: map[string]attr.Type{
								"type":           types.StringType,
								"principal_id":   types.StringType,
								"principal_type": types.StringType,
							},
						},
						"role_id": types.StringType,
					},
				},
			},
			"markings": schema.SetAttribute{
				ElementType: types.StringType,
				Description: "List of Markings applied to this Project.",
				Optional:    true,
			},
			"trash_status": schema.StringAttribute{
				Description: "Current trash status of the Project.",
				Computed:    true,
			},
		},
	}
}

// Create creates a new resource and sets the initial Terraform state.
func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	// Retrieve values from plan
	var plan projectResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.CreateProject(ctx, resp, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error creating the Project. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	if !plan.Markings.IsNull() {
		err = r.CreateProjectMarkings(ctx, resp, &plan)
		if err != nil {
			resp.Diagnostics.AddError("Error creating the Project markings. Please fix your plan if needed and re-apply.", err.Error())
		}
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *projectResource) CreateProject(ctx context.Context, resp *resource.CreateResponse, plan *projectResourceModel) error {
	var organizationsGoSlice []v2.CoreOrganizationRid
	diags := plan.Organizations.ElementsAs(context.Background(), &organizationsGoSlice, false)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return fmt.Errorf("failed to convert orgs to Go slice")
	}

	var roleGrants []ResourceRole
	diags = plan.ResourceRoles.ElementsAs(ctx, &roleGrants, false)

	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return fmt.Errorf("failed to convert roles to Go slice")
	}

	roleGrantsRequest := make(map[string][]v2.FilesystemPrincipalWithID)
	for _, roleGrant := range roleGrants {
		principal := v2.FilesystemPrincipalWithID{
			PrincipalID:   roleGrant.ResourceRolePrincipal.PrincipalID,
			PrincipalType: v2.CorePrincipalType(roleGrant.ResourceRolePrincipal.PrincipalType),
			Type:          roleGrant.ResourceRolePrincipal.Type,
		}
		roleGrantsRequest[roleGrant.RoleID] = append(roleGrantsRequest[roleGrant.RoleID], principal)
	}

	previewMode := constants.PreviewMode
	filesystemCreateProjectParams := v2.FilesystemCreateProjectParams{Preview: &previewMode}
	description := plan.Description.ValueString()

	httpResp, err := r.client.FilesystemCreateProject(ctx,
		&filesystemCreateProjectParams,
		v2.FilesystemCreateProjectJSONRequestBody{
			Description:      &description,
			DisplayName:      plan.DisplayName.ValueString(),
			OrganizationRids: &organizationsGoSlice,
			SpaceRid:         plan.SpaceRID.ValueString(),
			RoleGrants:       &roleGrantsRequest,
		})

	if err != nil {
		resp.Diagnostics.AddError("FilesystemCreateProject request failed", err.Error())
		return fmt.Errorf("FilesystemCreateProject request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemCreateProject response", err.Error())
			return fmt.Errorf("failed to format error logging from FilesystemCreateProject response: %w", err)
		}
		resp.Diagnostics.AddError("Response from FilesystemCreateProject was unsuccessful: ", returnString)
		return fmt.Errorf("response from FilesystemCreateProject was unsuccessful: %s", returnString)
	}

	//if success - take id from the response and update the state
	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from FilesystemCreateProject", err.Error())
		return fmt.Errorf("failed to parse response from FilesystemCreateProject: %w", err)
	}
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	//CREATE - do not save state if id is not saved
	if httpResponseBody.RID == "" {
		tflog.Error(ctx, "RID was not populated in response, "+
			"so Terraform best practice is NOT to update state as resource likely was not properly created")
		resp.Diagnostics.AddError("ID returned as empty",
			"ID was not populated in response, "+
				"so Terraform best practice is NOT to update state as resource likely was not properly created")
		return fmt.Errorf("ID returned as empty: %s", httpResponseBody.RID)
	}

	//set computed values
	plan.RID = types.StringValue(httpResponseBody.RID)
	plan.TrashStatus = types.StringValue(httpResponseBody.TrashStatus)
	return nil
}

func (r *projectResource) CreateProjectMarkings(ctx context.Context, resp *resource.CreateResponse, plan *projectResourceModel) error {
	var markingsGoSlice []string
	diags := plan.Markings.ElementsAs(context.Background(), &markingsGoSlice, false)

	if diags.HasError() {
		return fmt.Errorf("failed to convert planned markings to Go slice")
	}

	markingIdsToAdd := make([]v2.CoreMarkingID, 0)
	for _, markingID := range markingsGoSlice {
		markingUUID, err := uuid.Parse(markingID)
		if err != nil {
			return fmt.Errorf("error parsing uuid")
		}
		markingIdsToAdd = append(markingIdsToAdd, markingUUID)
	}

	previewMode := constants.PreviewMode
	filesystemAddMarkingParams := v2.FilesystemAddMarkingsParams{Preview: &previewMode}

	httpResp, err := r.client.FilesystemAddMarkings(ctx, plan.RID.ValueString(), &filesystemAddMarkingParams,
		v2.FilesystemAddMarkingsJSONRequestBody{
			MarkingIds: &markingIdsToAdd,
		})

	if err != nil {
		return fmt.Errorf("FilesystemAddMarkings request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusNoContent {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from FilesystemAddMarkings response: %w", err)
		}
		plan.Markings, diags = types.SetValueFrom(ctx, types.StringType, make([]string, 0))
		if diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return fmt.Errorf("failed to initialize project markings in plan")
		}
		return errors.New(returnString)
	}
	return nil
}

// Read refreshes the Terraform state with the latest data.
func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Get current state
	var state projectResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.ReadProject(ctx, resp, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Project.", err.Error())
		return
	}

	if resp.Diagnostics.Warnings().Contains(providerError.ResourceNotFoundWarning(state.RID.ValueString(), "Project")) {
		resp.State.RemoveResource(ctx)
		return
	}

	err = r.ReadOrganizations(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Project's Organizations.", err.Error())
	}

	err = r.ReadResourceRoles(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Project's role assignments.", err.Error())
	}

	err = r.ReadMarkings(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error reading the Project's Markings", err.Error())
	}

	// Set refreshed state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *projectResource) ReadProject(ctx context.Context, resp *resource.ReadResponse, state *projectResourceModel) error {
	httpResp, err := r.client.FilesystemGetProject(ctx, state.RID.ValueString())

	if err != nil {
		resp.Diagnostics.AddError("FilesystemGetProject request failed", err.Error())
		return fmt.Errorf("FilesystemGetProject request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusNotFound {
			resp.Diagnostics.Append(providerError.ResourceNotFoundWarning(state.RID.ValueString(), "Project"))
			return nil
		}
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemGetProject response", err.Error())
			return fmt.Errorf("failed to format error logging from FilesystemGetProject response: %w", err)
		}
		resp.Diagnostics.AddError("Response from FilesystemGetProject was unsuccessful: ", returnString)
		return fmt.Errorf("response from FilesystemGetProject was unsuccessful: %s", returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from FilesystemGetProject", err.Error())
		return fmt.Errorf("failed to parse response from FilesystemGetProject: %w", err)
	}

	//if success - take id from the response and update the state
	var httpResponseBody responseBody
	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	state.RID = types.StringValue(httpResponseBody.RID)
	state.SpaceRID = types.StringValue(httpResponseBody.SpaceRID)
	state.DisplayName = types.StringValue(httpResponseBody.DisplayName)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)
	state.TrashStatus = types.StringValue(httpResponseBody.TrashStatus)
	return nil
}

func (r *projectResource) ReadOrganizations(ctx context.Context, state *projectResourceModel) error {
	previewMode := constants.PreviewMode
	pageSize := constants.PageSize
	filesystemListOrganizationsOfProjectParams := v2.FilesystemListOrganizationsOfProjectParams{Preview: &previewMode, PageSize: &pageSize}
	httpResp, err := r.client.FilesystemListOrganizationsOfProject(ctx, state.RID.ValueString(), &filesystemListOrganizationsOfProjectParams)

	if err != nil {
		log.Fatal(err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from FilesystemListOrganizationsOfProject response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from FilesystemListOrganizationsOfProject: %w", err)
	}

	var httpListOrganizationsResponseBody listOrganizationsResponseBody
	if err := json.Unmarshal(bodyBytes, &httpListOrganizationsResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}

	state.Organizations, _ = types.SetValueFrom(ctx, types.StringType, httpListOrganizationsResponseBody.Data)
	return nil
}

func (r *projectResource) ReadResourceRoles(ctx context.Context, state *projectResourceModel) error {
	previewMode := constants.PreviewMode
	pageSize := constants.PageSize
	filesystemListResourceRolesParams := v2.FilesystemListResourceRolesParams{Preview: &previewMode, PageSize: &pageSize}
	httpResp, err := r.client.FilesystemListResourceRoles(ctx, state.RID.ValueString(), &filesystemListResourceRolesParams)

	if err != nil {
		return fmt.Errorf("FilesystemListResourceRoles request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from FilesystemListResourceRoles response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from FilesystemListResourceRoles: %w", err)
	}

	var httpListResourceRoles ResourceRolesResponse
	if err := json.Unmarshal(bodyBytes, &httpListResourceRoles); err != nil {
		return fmt.Errorf("error decoding response: %w", err)
	}
	// Build a slice of attr.Value for the set
	var resourceRolesValues []attr.Value
	for _, role := range httpListResourceRoles.Roles {
		// Build the inner object
		principalObj, _ := types.ObjectValue(
			map[string]attr.Type{
				"type":           types.StringType,
				"principal_id":   types.StringType,
				"principal_type": types.StringType,
			},
			map[string]attr.Value{
				"type":           types.StringValue(role.ResourceRolePrincipal.Type),
				"principal_id":   types.StringValue(role.ResourceRolePrincipal.PrincipalID),
				"principal_type": types.StringValue(role.ResourceRolePrincipal.PrincipalType),
			},
		)

		// Build the outer object
		roleObj, _ := types.ObjectValue(
			map[string]attr.Type{
				"resource_role_principal": types.ObjectType{
					AttrTypes: map[string]attr.Type{
						"type":           types.StringType,
						"principal_id":   types.StringType,
						"principal_type": types.StringType,
					},
				},
				"role_id": types.StringType,
			},
			map[string]attr.Value{
				"resource_role_principal": principalObj,
				"role_id":                 types.StringValue(role.RoleID),
			},
		)
		resourceRolesValues = append(resourceRolesValues, roleObj)
	}

	// Create the set from the slice of attr.Value
	resourceRolesSet, _ := types.SetValue(
		types.ObjectType{
			AttrTypes: map[string]attr.Type{
				"resource_role_principal": types.ObjectType{
					AttrTypes: map[string]attr.Type{
						"type":           types.StringType,
						"principal_id":   types.StringType,
						"principal_type": types.StringType,
					},
				},
				"role_id": types.StringType,
			},
		},
		resourceRolesValues,
	)

	// Update the state
	if len(resourceRolesValues) > 0 {
		state.ResourceRoles = resourceRolesSet
	}
	return nil
}

func (r *projectResource) ReadMarkings(ctx context.Context, state *projectResourceModel) error {
	previewMode := constants.PreviewMode
	pageSize := constants.PageSize
	filesystemListMarkingsOfResourceParams := v2.FilesystemListMarkingsOfResourceParams{Preview: &previewMode, PageSize: &pageSize}
	httpResp, err := r.client.FilesystemListMarkingsOfResource(ctx, state.RID.ValueString(), &filesystemListMarkingsOfResourceParams)

	if err != nil {
		return fmt.Errorf("FilesystemListMarkingsOfResource request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			return fmt.Errorf("failed to format error logging from FilesystemListMarkingsOfResource response: %w", err)
		}
		return errors.New(returnString)
	}

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		return fmt.Errorf("failed to parse response from FilesystemListResourceRoles: %w", err)
	}

	var httpListMarkingsResponseBody listMarkingsResponseBody
	if err := json.Unmarshal(bodyBytes, &httpListMarkingsResponseBody); err != nil {
		return fmt.Errorf("error decoding response: %w", err)

	}

	if len(httpListMarkingsResponseBody.Data) > 0 {
		state.Markings, _ = types.SetValueFrom(ctx, types.StringType, httpListMarkingsResponseBody.Data)
	}
	return nil
}

// Update updates the resource and sets the updated Terraform state on success.
func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Retrieve values from plan
	var plan projectResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state
	var state projectResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.UpdateProject(ctx, resp, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Project. Please fix your plan if needed and re-apply", err.Error())
		return
	}

	err = r.UpdateProjectMarkings(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Project's markings. Please fix your plan if needed and re-apply", err.Error())
	}

	err = r.UpdateProjectRoles(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Project's roles. Please fix your plan if needed and re-apply", err.Error())
	}

	err = r.UpdateProjectOrganizations(ctx, &plan, &state, resp)
	if err != nil {
		resp.Diagnostics.AddError("Error updating the Project's organizations. Please fix your plan if needed and re-apply", err.Error())
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

func (r *projectResource) UpdateProject(ctx context.Context, resp *resource.UpdateResponse, plan *projectResourceModel, state *projectResourceModel) error {
	previewMode := constants.PreviewMode

	filesystemReplaceProjectParams := v2.FilesystemReplaceProjectParams{Preview: &previewMode}
	description := plan.Description.ValueString()

	httpResp, err := r.client.FilesystemReplaceProject(ctx, state.RID.ValueString(), &filesystemReplaceProjectParams, v2.FilesystemReplaceProjectJSONRequestBody{
		DisplayName: plan.DisplayName.ValueString(),
		Description: &description,
	})

	if err != nil {
		resp.Diagnostics.AddError("FilesystemReplaceProject request failed", err.Error())
		return fmt.Errorf("FilesystemReplaceProject request failed: %w", err)
	}

	// Check the response status code
	if httpResp.StatusCode != http.StatusOK {
		returnString, err := providerError.FormatHTTPError(httpResp)
		if err != nil {
			resp.Diagnostics.AddError("Failed to format error logging from FilesystemReplaceProject response", err.Error())
			return fmt.Errorf("failed to format error logging from FilesystemReplaceProject response: %w", err)
		}
		resp.Diagnostics.AddError("Response from FilesystemReplaceProject was unsuccessful: ", returnString)
		return fmt.Errorf("response from FilesystemReplaceProject was unsuccessful: %s", returnString)
	}

	//read body and then close
	var httpResponseBody responseBody

	bodyBytes, err := helper.ExtractBodyFromResponse(httpResp)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse response from FilesystemReplaceProject", err.Error())
		return fmt.Errorf("failed to parse response from FilesystemReplaceProject: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &httpResponseBody); err != nil {
		resp.Diagnostics.AddError("Error decoding response",
			fmt.Sprintf("... details ... %s", err))
		return fmt.Errorf("error decoding response: %w", err)
	}

	// Update the state with the new values

	state.DisplayName = types.StringValue(httpResponseBody.DisplayName)
	state.Description = helper.HandleEmptyFieldString(httpResponseBody.Description)

	return nil
}

func (r *projectResource) UpdateProjectMarkings(ctx context.Context, plan *projectResourceModel, state *projectResourceModel, resp *resource.UpdateResponse) error {
	var oldMarkings []string
	var newMarkings []string
	previewMode := constants.PreviewMode

	if !state.Markings.IsNull() {
		diags := state.Markings.ElementsAs(ctx, &oldMarkings, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert project markings to Go slice")
		}
	}

	if !plan.Markings.IsNull() {
		diags := plan.Markings.ElementsAs(ctx, &newMarkings, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert project markings to Go slice")
		}
	}

	if !slices.Equal(oldMarkings, newMarkings) {
		// Determine members to add and remove
		markingsToAdd, markingsToRemove := helper.FindStringSliceDiff(oldMarkings, newMarkings)
		if len(markingsToAdd) != 0 {
			markingIdsToAdd := make([]v2.CoreMarkingID, len(markingsToAdd))
			for i, markingID := range markingsToAdd {
				markingUUID, err := uuid.Parse(markingID)
				if err != nil {
					return fmt.Errorf("error parsing marking UUID: %w", err)
				}
				markingIdsToAdd[i] = markingUUID
			}

			filesystemAddMarkingParams := v2.FilesystemAddMarkingsParams{Preview: &previewMode}
			httpResp, err := r.client.FilesystemAddMarkings(ctx, state.RID.ValueString(), &filesystemAddMarkingParams, v2.FilesystemAddMarkingsJSONRequestBody{
				MarkingIds: &markingIdsToAdd,
			})

			if err != nil {
				return fmt.Errorf("FilesystemAddMarkings request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from FilesystemAddMarkings response: %w", err)
				}
				return errors.New(returnString)
			}
		}
		if len(markingsToRemove) != 0 && !r.deletionsDisabled {
			markingIdsToRemove := make([]v2.CoreMarkingID, len(markingsToRemove))
			for i, markingID := range markingsToRemove {
				markingUUID, err := uuid.Parse(markingID)
				if err != nil {
					return fmt.Errorf("error parsing marking UUID: %w", err)
				}
				markingIdsToRemove[i] = markingUUID
			}

			filesystemRemoveMarkingParams := v2.FilesystemRemoveMarkingsParams{Preview: &previewMode}
			httpResp, err := r.client.FilesystemRemoveMarkings(ctx, state.RID.ValueString(), &filesystemRemoveMarkingParams, v2.FilesystemRemoveMarkingsJSONRequestBody{
				MarkingIds: &markingIdsToRemove,
			})

			if err != nil {
				return fmt.Errorf("FilesystemRemoveMarkings request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from FilesystemRemoveMarkings response: %w", err)
				}
				return errors.New(returnString)
			}
		} else if len(markingsToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found markings defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, marking-removal operations will not be applied.")
		}
		state.Markings = plan.Markings
	}
	return nil
}

func (r *projectResource) UpdateProjectRoles(ctx context.Context, plan *projectResourceModel, state *projectResourceModel, resp *resource.UpdateResponse) error {
	previewMode := constants.PreviewMode
	var oldResourceRoles []ResourceRole

	if !state.ResourceRoles.IsNull() {
		diags := state.ResourceRoles.ElementsAs(ctx, &oldResourceRoles, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert project roles to Go slice")
		}
	}

	var newResourceRoles []ResourceRole
	if !plan.ResourceRoles.IsNull() {
		diags := plan.ResourceRoles.ElementsAs(ctx, &newResourceRoles, false)
		if diags.HasError() {
			return fmt.Errorf("failed to convert project roles to Go slice")
		}
	}

	if !slices.Equal(oldResourceRoles, newResourceRoles) {
		// Determine members to add and remove
		rolesToAdd, rolesToRemove := DiffResourceRoles(oldResourceRoles, newResourceRoles)
		if len(rolesToAdd) != 0 {
			roleUpdates := make([]v2.FilesystemResourceRole, len(rolesToAdd))

			for i, role := range rolesToAdd {
				principal := v2.FilesystemResourceRolePrincipal{}
				if role.ResourceRolePrincipal.Type == "principalWithId" {
					err := principal.FromFilesystemPrincipalWithID(v2.FilesystemPrincipalWithID{
						PrincipalID:   role.ResourceRolePrincipal.PrincipalID,
						PrincipalType: v2.CorePrincipalType(role.ResourceRolePrincipal.PrincipalType),
						Type:          role.ResourceRolePrincipal.Type,
					})
					if err != nil {
						return fmt.Errorf("FilesystemPrincipalWithID request failed: %w", err)
					}
				}

				if role.ResourceRolePrincipal.Type == "everyone" {
					err := principal.FromFilesystemEveryone(v2.FilesystemEveryone{
						Type: role.ResourceRolePrincipal.Type,
					})
					roleUpdates[i] = v2.FilesystemResourceRole{
						ResourceRolePrincipal: principal,
						RoleID:                role.RoleID,
					}
					if err != nil {
						return fmt.Errorf("FilesystemPrincipalWithID request failed: %w", err)
					}
				}
				roleUpdates[i] = v2.FilesystemResourceRole{
					ResourceRolePrincipal: principal,
					RoleID:                role.RoleID,
				}
			}

			filesystemAddResourceRoleParams := v2.FilesystemAddResourceRolesParams{Preview: &previewMode}
			httpResp, err := r.client.FilesystemAddResourceRoles(ctx, state.RID.ValueString(), &filesystemAddResourceRoleParams, v2.FilesystemAddResourceRolesJSONRequestBody{
				Roles: &roleUpdates,
			})

			if err != nil {
				return fmt.Errorf("FilesystemAddResourceRoles request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					log.Fatal(err)
				}
				return errors.New(returnString)
			}
		}
		if len(rolesToRemove) != 0 && !r.deletionsDisabled {
			roleUpdates := make([]v2.FilesystemResourceRole, len(rolesToRemove))

			for i, role := range rolesToRemove {
				principal := v2.FilesystemResourceRolePrincipal{}
				if role.ResourceRolePrincipal.Type == "principalWithId" {
					err := principal.FromFilesystemPrincipalWithID(v2.FilesystemPrincipalWithID{
						PrincipalID:   role.ResourceRolePrincipal.PrincipalID,
						PrincipalType: v2.CorePrincipalType(role.ResourceRolePrincipal.PrincipalType),
						Type:          role.ResourceRolePrincipal.Type,
					})
					if err != nil {
						return fmt.Errorf("FilesystemPrincipalWithID request failed: %w", err)
					}
				}

				if role.ResourceRolePrincipal.Type == "everyone" {
					err := principal.FromFilesystemEveryone(v2.FilesystemEveryone{
						Type: role.ResourceRolePrincipal.Type,
					})
					roleUpdates[i] = v2.FilesystemResourceRole{
						ResourceRolePrincipal: principal,
						RoleID:                role.RoleID,
					}
					if err != nil {
						return fmt.Errorf("FilesystemPrincipalWithID request failed: %w", err)
					}
				}
				roleUpdates[i] = v2.FilesystemResourceRole{
					ResourceRolePrincipal: principal,
					RoleID:                role.RoleID,
				}
			}

			filesystemRemoveResourceRoleParams := v2.FilesystemRemoveResourceRolesParams{Preview: &previewMode}
			httpResp, err := r.client.FilesystemRemoveResourceRoles(ctx, state.RID.ValueString(), &filesystemRemoveResourceRoleParams, v2.FilesystemRemoveResourceRolesJSONRequestBody{
				Roles: &roleUpdates,
			})

			if err != nil {
				return fmt.Errorf("FilesystemRemoveResourceRoles request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					log.Fatal(err)
				}
				return errors.New(returnString)
			}
		} else if len(rolesToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found roles defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, role-removal operations will not be applied.")
		}
		state.ResourceRoles = plan.ResourceRoles
	}
	return nil
}
func (r *projectResource) UpdateProjectOrganizations(ctx context.Context, plan *projectResourceModel, state *projectResourceModel, resp *resource.UpdateResponse) error {
	previewMode := constants.PreviewMode
	var oldOrganizations []string
	var newOrganizations []string

	diags := state.Organizations.ElementsAs(ctx, &oldOrganizations, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert organizations to Go slice")
	}
	diags = plan.Organizations.ElementsAs(ctx, &newOrganizations, false)
	if diags.HasError() {
		return fmt.Errorf("failed to convert organizations to Go slice")
	}

	if !slices.Equal(oldOrganizations, newOrganizations) {
		// Determine members to add and remove
		organizationToAdd, organizationsToRemove := helper.FindStringSliceDiff(oldOrganizations, newOrganizations)
		if len(organizationToAdd) != 0 {
			filesystemAddOrganizationsParams := v2.FilesystemAddOrganizationsParams{Preview: &previewMode}
			httpResp, err := r.client.FilesystemAddOrganizations(ctx, state.RID.ValueString(), &filesystemAddOrganizationsParams, v2.FilesystemAddOrganizationsJSONRequestBody{
				OrganizationRids: &organizationToAdd,
			})

			if err != nil {
				return fmt.Errorf("FilesystemAddOrganizations request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from FilesystemAddOrganizations response: %w", err)
				}
				return errors.New(returnString)
			}
		}
		if len(organizationsToRemove) != 0 && !r.deletionsDisabled {
			filesystemRemoveOrganizationsParams := v2.FilesystemRemoveOrganizationsParams{Preview: &previewMode}
			httpResp, err := r.client.FilesystemRemoveOrganizations(ctx, state.RID.ValueString(), &filesystemRemoveOrganizationsParams, v2.FilesystemRemoveOrganizationsJSONRequestBody{
				OrganizationRids: &organizationsToRemove,
			})

			if err != nil {
				return fmt.Errorf("FilesystemRemoveOrganizations request failed: %w", err)
			}

			// Check the response status code
			if httpResp.StatusCode != http.StatusNoContent {
				returnString, err := providerError.FormatHTTPError(httpResp)
				if err != nil {
					return fmt.Errorf("failed to format error logging from FilesystemRemoveOrganizations response: %w", err)
				}
				return errors.New(returnString)
			}
		} else if len(organizationsToRemove) != 0 {
			resp.Diagnostics.AddWarning("Found organization members defined in the state that are not in the plan.",
				"Since `deletions_disabled` is set to true, organization-member-removal operations will not be applied.")
		}
		state.Organizations = plan.Organizations
	}
	return nil
}

// Delete deletes the resource and removes the Terraform state on success.
func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// If deletions are disabled, error.
	if r.deletionsDisabled {
		resp.Diagnostics.AddError("Tried to perform a deletion when the deletions_disabled flag was set to true.",
			fmt.Sprintf("Project with name %s and rid %s will not be deleted.", state.DisplayName.ValueString(), state.RID.ValueString()))
		return
	}

	if state.TrashStatus.ValueString() == string(v2.NOTTRASHED) {
		err := r.DeleteResource(ctx, resp, &state)
		if err != nil {
			resp.Diagnostics.AddError("Error deleting the Project", err.Error())
			return
		}
	}

	//if initial delete is successful, now we can check and permanently delete the resource.
	//this should also work for if the resource was already trashed directly or by ancestor outside of TF
	// and we are just permanently deleting it now. we should return if this fails
	if state.TrashStatus.ValueString() == string(v2.DIRECTLYTRASHED) || state.TrashStatus.ValueString() == string(v2.ANCESTORTRASHED) {
		err := r.PermanentlyDeleteResource(ctx, resp, &state)
		if err != nil {
			resp.Diagnostics.AddError("Error permanently deleting the project resource", err.Error())
		}
		// we want to return here as we do not want to destroy the resource if the permanent delete fails. since trash_status is a
		// computed value, we do not need to worry in case it doesn't get persisted in state now as it will on the next read of the resource
		return
	}
}

func (r *projectResource) DeleteResource(ctx context.Context, resp *resource.DeleteResponse, state *projectResourceModel) error {
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

func (r *projectResource) PermanentlyDeleteResource(ctx context.Context, resp *resource.DeleteResponse, state *projectResourceModel) error {
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

// ImportState imports an existing marking into Terraform state.
func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// The import ID is expected to be the marking RID
	projectID := req.ID

	// Validate the ID format (optional, can add your own validation logic)
	if projectID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"The import ID must be the project RID",
		)
		return
	}

	tflog.Info(ctx, fmt.Sprintf("Importing project with RID %s", projectID))

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rid"), projectID)...)
}

func DiffResourceRoles(oldResourceRoles, newResourceRoles []ResourceRole) (added, removed []ResourceRole) {
	oldMap := make(map[string]ResourceRole)
	newMap := make(map[string]ResourceRole)

	// Helper to create a unique key for each RoleResource
	makeKey := func(r ResourceRole) string {
		p := r.ResourceRolePrincipal
		return r.RoleID + "|" + p.Type + "|" + p.PrincipalID + "|" + p.PrincipalType
	}

	for _, r := range oldResourceRoles {
		oldMap[makeKey(r)] = r
	}
	for _, r := range newResourceRoles {
		newMap[makeKey(r)] = r
	}

	// Find added
	for k, r := range newMap {
		if _, exists := oldMap[k]; !exists {
			added = append(added, r)
		}
	}
	// Find removed
	for k, r := range oldMap {
		if _, exists := newMap[k]; !exists {
			removed = append(removed, r)
		}
	}
	return added, removed
}
