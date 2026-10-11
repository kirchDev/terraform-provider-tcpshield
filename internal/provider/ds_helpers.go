package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

// Data sources reuse the resource models, so their attribute maps mirror the
// resource schemas with every attribute Computed. The constructors below keep
// those maps short.

func dsStr(desc string) schema.StringAttribute {
	return schema.StringAttribute{MarkdownDescription: desc, Computed: true}
}

func dsSensitiveStr(desc string) schema.StringAttribute {
	return schema.StringAttribute{MarkdownDescription: desc, Computed: true, Sensitive: true}
}

func dsInt(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{MarkdownDescription: desc, Computed: true}
}

func dsBool(desc string) schema.BoolAttribute {
	return schema.BoolAttribute{MarkdownDescription: desc, Computed: true}
}

func dsStrList(desc string) schema.ListAttribute {
	return schema.ListAttribute{MarkdownDescription: desc, Computed: true, ElementType: stringType}
}

func dsRequiredInt(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{MarkdownDescription: desc, Required: true}
}

// dsList wraps an entity's attribute map as a computed list of objects.
func dsList(desc string, attrs map[string]schema.Attribute) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		MarkdownDescription: desc,
		Computed:            true,
		NestedObject:        schema.NestedAttributeObject{Attributes: attrs},
	}
}

// with returns a copy of attrs with the given attributes replaced or added — a
// single-entity data source turns the computed id into its required lookup key.
func with(attrs map[string]schema.Attribute, over map[string]schema.Attribute) map[string]schema.Attribute {
	out := make(map[string]schema.Attribute, len(attrs)+len(over))
	for k, v := range attrs {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
	return out
}
