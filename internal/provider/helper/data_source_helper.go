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
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	resourceSchema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// ConvertResourceSchemaToDataSource converts a resource schema to a data source schema
// by marking all attributes as computed and removing Required/Optional flags.
// It automatically detects and marks the identifier field as required based on these rules:
// 1. If there's a field called "id" or "rid", use that
// 2. Otherwise, find the first top-level field ending in "_id" or "_rid"
func ConvertResourceSchemaToDataSource(resourceSchema resourceSchema.Schema, description string) schema.Schema {
	identifierField := detectIdentifierField(resourceSchema.Attributes)

	return schema.Schema{
		Description: description,
		Attributes:  convertResourceAttributesToDataSource(resourceSchema.Attributes, identifierField),
		Blocks:      convertResourceBlocksToDataSource(resourceSchema.Blocks),
	}
}

// detectIdentifierField finds the identifier field based on naming conventions
func detectIdentifierField(attrs map[string]resourceSchema.Attribute) string {
	// Rule 1: Check for exact matches "id" or "rid"
	if _, ok := attrs["id"]; ok {
		return "id"
	}
	if _, ok := attrs["rid"]; ok {
		return "rid"
	}

	// Rule 2: Find first field ending in "_id" or "_rid"
	for name := range attrs {
		if strings.HasSuffix(name, "_id") || strings.HasSuffix(name, "_rid") {
			return name
		}
	}

	// No identifier found - this shouldn't happen in a well-formed resource
	return ""
}

// convertResourceAttributesToDataSource converts resource attributes to data source attributes
func convertResourceAttributesToDataSource(attrs map[string]resourceSchema.Attribute, identifierField string) map[string]schema.Attribute {
	result := make(map[string]schema.Attribute)
	for name, attr := range attrs {
		if name == identifierField {
			result[name] = convertToRequiredDataSourceAttribute(attr, name)
		} else {
			result[name] = convertResourceAttributeToDataSource(attr)
		}
	}
	return result
}

// convertToRequiredDataSourceAttribute converts a resource attribute to a required data source attribute
func convertToRequiredDataSourceAttribute(attr resourceSchema.Attribute, fieldName string) schema.Attribute {
	switch a := attr.(type) {
	case resourceSchema.StringAttribute:
		return schema.StringAttribute{
			Description:         getIdentifierDescription(fieldName, a.Description),
			MarkdownDescription: a.MarkdownDescription,
			Required:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.Int64Attribute:
		return schema.Int64Attribute{
			Description:         getIdentifierDescription(fieldName, a.Description),
			MarkdownDescription: a.MarkdownDescription,
			Required:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.Float64Attribute:
		return schema.Float64Attribute{
			Description:         getIdentifierDescription(fieldName, a.Description),
			MarkdownDescription: a.MarkdownDescription,
			Required:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.NumberAttribute:
		return schema.NumberAttribute{
			Description:         getIdentifierDescription(fieldName, a.Description),
			MarkdownDescription: a.MarkdownDescription,
			Required:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	default:
		// For other types, fall back to computed
		return convertResourceAttributeToDataSource(attr)
	}
}

// getIdentifierDescription updates the description to indicate this is a required lookup field
func getIdentifierDescription(fieldName, originalDescription string) string {
	if originalDescription == "" {
		return "The identifier used to look up this resource."
	}
	// If the description doesn't already mention it's for lookup, add a note
	if !strings.Contains(strings.ToLower(originalDescription), "look up") &&
		!strings.Contains(strings.ToLower(originalDescription), "lookup") {
		return originalDescription + " (Required for data source lookup.)"
	}
	return originalDescription
}

// convertResourceAttributeToDataSource converts a single resource attribute to a data source attribute
func convertResourceAttributeToDataSource(attr resourceSchema.Attribute) schema.Attribute {
	switch a := attr.(type) {
	case resourceSchema.StringAttribute:
		return schema.StringAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.BoolAttribute:
		return schema.BoolAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.Int64Attribute:
		return schema.Int64Attribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.Float64Attribute:
		return schema.Float64Attribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.NumberAttribute:
		return schema.NumberAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.ListAttribute:
		return schema.ListAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			ElementType:         a.ElementType,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.SetAttribute:
		return schema.SetAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			ElementType:         a.ElementType,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.MapAttribute:
		return schema.MapAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			ElementType:         a.ElementType,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.ObjectAttribute:
		return schema.ObjectAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			AttributeTypes:      a.AttributeTypes,
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.SingleNestedAttribute:
		return schema.SingleNestedAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			Attributes:          convertResourceAttributesToDataSource(a.Attributes, ""), // No identifier in nested
			Computed:            true,
			Sensitive:           a.Sensitive,
			DeprecationMessage:  a.DeprecationMessage,
		}
	case resourceSchema.ListNestedAttribute:
		return schema.ListNestedAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			NestedObject: schema.NestedAttributeObject{
				Attributes: convertResourceAttributesToDataSource(a.NestedObject.Attributes, ""), // No identifier in nested
			},
			Computed:           true,
			Sensitive:          a.Sensitive,
			DeprecationMessage: a.DeprecationMessage,
		}
	case resourceSchema.SetNestedAttribute:
		return schema.SetNestedAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			NestedObject: schema.NestedAttributeObject{
				Attributes: convertResourceAttributesToDataSource(a.NestedObject.Attributes, ""), // No identifier in nested
			},
			Computed:           true,
			Sensitive:          a.Sensitive,
			DeprecationMessage: a.DeprecationMessage,
		}
	case resourceSchema.MapNestedAttribute:
		return schema.MapNestedAttribute{
			Description:         a.Description,
			MarkdownDescription: a.MarkdownDescription,
			NestedObject: schema.NestedAttributeObject{
				Attributes: convertResourceAttributesToDataSource(a.NestedObject.Attributes, ""), // No identifier in nested
			},
			Computed:           true,
			Sensitive:          a.Sensitive,
			DeprecationMessage: a.DeprecationMessage,
		}
	default:
		// For any unknown types, return as-is
		// This shouldn't happen with standard framework types
		return attr
	}
}

// convertResourceBlocksToDataSource converts resource blocks to data source blocks
func convertResourceBlocksToDataSource(blocks map[string]resourceSchema.Block) map[string]schema.Block {
	if len(blocks) == 0 {
		return nil
	}

	result := make(map[string]schema.Block)
	for name, block := range blocks {
		result[name] = convertResourceBlockToDataSource(block)
	}
	return result
}

// convertResourceBlockToDataSource converts a single resource block to a data source block
func convertResourceBlockToDataSource(block resourceSchema.Block) schema.Block {
	switch b := block.(type) {
	case resourceSchema.ListNestedBlock:
		return schema.ListNestedBlock{
			Description:         b.Description,
			MarkdownDescription: b.MarkdownDescription,
			NestedObject: schema.NestedBlockObject{
				Attributes: convertResourceAttributesToDataSource(b.NestedObject.Attributes, ""), // No identifier in nested
				Blocks:     convertResourceBlocksToDataSource(b.NestedObject.Blocks),
			},
			DeprecationMessage: b.DeprecationMessage,
		}
	case resourceSchema.SetNestedBlock:
		return schema.SetNestedBlock{
			Description:         b.Description,
			MarkdownDescription: b.MarkdownDescription,
			NestedObject: schema.NestedBlockObject{
				Attributes: convertResourceAttributesToDataSource(b.NestedObject.Attributes, ""), // No identifier in nested
				Blocks:     convertResourceBlocksToDataSource(b.NestedObject.Blocks),
			},
			DeprecationMessage: b.DeprecationMessage,
		}
	case resourceSchema.SingleNestedBlock:
		return schema.SingleNestedBlock{
			Description:         b.Description,
			MarkdownDescription: b.MarkdownDescription,
			Attributes:          convertResourceAttributesToDataSource(b.Attributes, ""), // No identifier in nested
			Blocks:              convertResourceBlocksToDataSource(b.Blocks),
			DeprecationMessage:  b.DeprecationMessage,
		}
	default:
		// For any unknown types, return as-is
		return block
	}
}
