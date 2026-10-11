package provider

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

// TCPShield has three network firewalls — by IP/CIDR, by ASN and by country —
// with the same shape: an entry is a key plus a whitelist flag, there is no
// single-entry read and no update. One implementation serves all three; a
// firewallKind names what differs.
type firewallKind struct {
	typeName   string // resource / data source suffix
	collection string // path below /networks/{id}
	key        string // the attribute and JSON field that identifies an entry
	keyIsInt   bool
	keyDesc    string
	title      string
	// country entries also carry the resolved code and name.
	country bool
}

var (
	ipFirewall = firewallKind{
		typeName: "firewall_rule", collection: "/firewall", key: "cidr",
		keyDesc: "CIDR range, e.g. `203.0.113.0/24`; a single address needs `/32`. The API rejects a bare IP (`Invalid Cidr formatting`) and keeps host bits as written. The plan caps the number of entries (Pro: 5, Enterprise: 50) — one past the cap fails at apply.", title: "IP firewall",
	}
	asnFirewall = firewallKind{
		typeName: "asn_firewall_rule", collection: "/asnFirewall", key: "asn", keyIsInt: true,
		keyDesc: "Autonomous system number, e.g. `13335`.", title: "ASN firewall",
	}
	countryFirewall = firewallKind{
		typeName: "country_firewall_rule", collection: "/countryFirewall", key: "country_id", keyIsInt: true,
		keyDesc: "TCPShield's numeric country id (see `country_code` / `country_name` on an existing entry).", title: "country firewall",
		country: true,
	}
)

// apiFirewallEntry covers all three kinds; each uses its own key field.
type apiFirewallEntry struct {
	ID          int64      `json:"id"`
	CIDR        string     `json:"cidr"`
	ASN         int64      `json:"asn"`
	CountryID   int64      `json:"country_id"`
	CountryCode string     `json:"country_code"`
	CountryName string     `json:"country_name"`
	Whitelist   bool       `json:"whitelist"`
	CreatedAt   flexString `json:"created_at"`
}

func (k firewallKind) keyValue(e apiFirewallEntry) attr.Value {
	switch k.key {
	case "asn":
		return types.Int64Value(e.ASN)
	case "country_id":
		return types.Int64Value(e.CountryID)
	default:
		return types.StringValue(e.CIDR)
	}
}

// NewFirewallRuleResource manages an IP/CIDR firewall entry.
func NewFirewallRuleResource() resource.Resource { return &firewallResource{kind: ipFirewall} }

// NewASNFirewallRuleResource manages an ASN firewall entry.
func NewASNFirewallRuleResource() resource.Resource { return &firewallResource{kind: asnFirewall} }

// NewCountryFirewallRuleResource manages a country firewall entry.
func NewCountryFirewallRuleResource() resource.Resource {
	return &firewallResource{kind: countryFirewall}
}

type firewallResource struct {
	resourceBase
	kind firewallKind
}

var (
	_ resource.Resource                = (*firewallResource)(nil)
	_ resource.ResourceWithImportState = (*firewallResource)(nil)
)

func (r *firewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.kind.typeName
}

func (r *firewallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"network_id": parentIDAttr("Network the entry belongs to."),
		"id":         idAttr("Numeric entry id."),
		"whitelist": schema.BoolAttribute{
			MarkdownDescription: "`true` allows the source, `false` (default) blocks it.",
			Optional:            true,
			Computed:            true,
			Default:             booldefault.StaticBool(false),
			PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
		},
		"created_at": computedStr("Creation timestamp."),
	}
	if r.kind.keyIsInt {
		attrs[r.kind.key] = schema.Int64Attribute{
			MarkdownDescription: r.kind.keyDesc,
			Required:            true,
			PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
		}
	} else {
		attrs[r.kind.key] = schema.StringAttribute{
			MarkdownDescription: r.kind.keyDesc,
			Required:            true,
			Validators:          []validator.String{cidrValidator{}},
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
	}
	if r.kind.country {
		attrs["country_code"] = computedStr("ISO country code TCPShield resolved `country_id` to.")
		attrs["country_name"] = computedStr("Country name TCPShield resolved `country_id` to.")
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: fmt.Sprintf("An entry in a network's %s. The API has no update for entries, so every change replaces the entry.", r.kind.title),
		Attributes:          attrs,
	}
}

// attrGetter is the read half shared by tfsdk.Plan and tfsdk.State.
type attrGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

func getInt64(ctx context.Context, src attrGetter, name string, diags *diag.Diagnostics) int64 {
	var v types.Int64
	diags.Append(src.GetAttribute(ctx, path.Root(name), &v)...)
	return v.ValueInt64()
}

func (r *firewallResource) list(ctx context.Context, networkID int64) ([]apiFirewallEntry, error) {
	var list []apiFirewallEntry
	err := r.client.Get(ctx, networkPath(networkID, r.kind.collection), &list)
	return list, err
}

func (r *firewallResource) setState(ctx context.Context, s *tfsdk.State, diags *diag.Diagnostics, networkID int64, e apiFirewallEntry) {
	diags.Append(s.SetAttribute(ctx, path.Root("network_id"), networkID)...)
	diags.Append(s.SetAttribute(ctx, path.Root("id"), e.ID)...)
	diags.Append(s.SetAttribute(ctx, path.Root(r.kind.key), r.kind.keyValue(e))...)
	diags.Append(s.SetAttribute(ctx, path.Root("whitelist"), e.Whitelist)...)
	diags.Append(s.SetAttribute(ctx, path.Root("created_at"), string(e.CreatedAt))...)
	if r.kind.country {
		diags.Append(s.SetAttribute(ctx, path.Root("country_code"), e.CountryCode)...)
		diags.Append(s.SetAttribute(ctx, path.Root("country_name"), e.CountryName)...)
	}
}

func (r *firewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	networkID := getInt64(ctx, req.Plan, "network_id", &resp.Diagnostics)
	var whitelist types.Bool
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("whitelist"), &whitelist)...)

	var key attr.Value
	b := body{"whitelist": whitelist.ValueBool()}
	if r.kind.keyIsInt {
		var v types.Int64
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(r.kind.key), &v)...)
		b[r.kind.key], key = v.ValueInt64(), v
	} else {
		var v types.String
		resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root(r.kind.key), &v)...)
		b[r.kind.key], key = v.ValueString(), v
	}
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := r.client.Write(ctx, http.MethodPost, networkPath(networkID, r.kind.collection), b, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating "+r.kind.title+" entry", err.Error())
		return
	}

	list, err := r.list(ctx, networkID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.kind.title, err.Error())
		return
	}
	id, idOK := client.CreatedID(raw)
	var found *apiFirewallEntry
	for i := range list {
		e := &list[i]
		if idOK && e.ID == id || !idOK && r.kind.keyValue(*e).Equal(key) && (found == nil || e.ID > found.ID) {
			found = e
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("Error locating created "+r.kind.title+" entry",
			fmt.Sprintf("the entry was created but GET %s does not list it", networkPath(networkID, r.kind.collection)))
		return
	}
	r.setState(ctx, &resp.State, &resp.Diagnostics, networkID, *found)
}

func (r *firewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	networkID := getInt64(ctx, req.State, "network_id", &resp.Diagnostics)
	id := getInt64(ctx, req.State, "id", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	list, err := r.list(ctx, networkID)
	if client.NotFound(err) {
		resp.State.RemoveResource(ctx) // the network itself is gone
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading "+r.kind.title, err.Error())
		return
	}
	for _, e := range list {
		if e.ID == id {
			r.setState(ctx, &resp.State, &resp.Diagnostics, networkID, e)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *firewallResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Every attribute forces replacement, so the framework never calls this.
	resp.Diagnostics.AddError("Update not supported", "Firewall entries cannot be updated in place.")
}

func (r *firewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	networkID := getInt64(ctx, req.State, "network_id", &resp.Diagnostics)
	id := getInt64(ctx, req.State, "id", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, networkPath(networkID, fmt.Sprintf("%s/%d", r.kind.collection, id))); err != nil {
		resp.Diagnostics.AddError("Error deleting "+r.kind.title+" entry", err.Error())
	}
}

func (r *firewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportID(req.ID, "network_id", "entry_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[1])...)
}

// --- data sources ---

// NewFirewallRulesDataSource lists a network's IP firewall.
func NewFirewallRulesDataSource() datasource.DataSource {
	return &firewallRulesDataSource{kind: ipFirewall}
}

// NewASNFirewallRulesDataSource lists a network's ASN firewall.
func NewASNFirewallRulesDataSource() datasource.DataSource {
	return &firewallRulesDataSource{kind: asnFirewall}
}

// NewCountryFirewallRulesDataSource lists a network's country firewall.
func NewCountryFirewallRulesDataSource() datasource.DataSource {
	return &firewallRulesDataSource{kind: countryFirewall}
}

type firewallRulesDataSource struct {
	dataSourceBase
	kind firewallKind
}

func (d *firewallRulesDataSource) entryAttrs() map[string]dschema.Attribute {
	attrs := map[string]dschema.Attribute{
		"id":         dsInt("Numeric entry id."),
		"whitelist":  dsBool("`true` allows the source, `false` blocks it."),
		"created_at": dsStr("Creation timestamp."),
	}
	if d.kind.keyIsInt {
		attrs[d.kind.key] = dsInt(d.kind.keyDesc)
	} else {
		attrs[d.kind.key] = dsStr(d.kind.keyDesc)
	}
	if d.kind.country {
		attrs["country_code"] = dsStr("ISO country code.")
		attrs["country_name"] = dsStr("Country name.")
	}
	return attrs
}

func (d *firewallRulesDataSource) entryTypes() map[string]attr.Type {
	t := map[string]attr.Type{
		"id":         types.Int64Type,
		"whitelist":  types.BoolType,
		"created_at": types.StringType,
	}
	if d.kind.keyIsInt {
		t[d.kind.key] = types.Int64Type
	} else {
		t[d.kind.key] = types.StringType
	}
	if d.kind.country {
		t["country_code"] = types.StringType
		t["country_name"] = types.StringType
	}
	return t
}

func (d *firewallRulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.kind.typeName + "s"
}

func (d *firewallRulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: fmt.Sprintf("Lists the entries of a network's %s.", d.kind.title),
		Attributes: map[string]dschema.Attribute{
			"network_id": dsRequiredInt("Network to list."),
			"rules":      dsList("The entries.", d.entryAttrs()),
		},
	}
}

func (d *firewallRulesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	networkID := getInt64(ctx, req.Config, "network_id", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []apiFirewallEntry
	if err := d.client.Get(ctx, networkPath(networkID, d.kind.collection), &list); err != nil {
		resp.Diagnostics.AddError("Error listing "+d.kind.title, err.Error())
		return
	}
	objType := types.ObjectType{AttrTypes: d.entryTypes()}
	elems := make([]attr.Value, 0, len(list))
	for _, e := range list {
		vals := map[string]attr.Value{
			"id":         types.Int64Value(e.ID),
			"whitelist":  types.BoolValue(e.Whitelist),
			"created_at": e.CreatedAt.value(),
			d.kind.key:   d.kind.keyValue(e),
		}
		if d.kind.country {
			vals["country_code"] = types.StringValue(e.CountryCode)
			vals["country_name"] = types.StringValue(e.CountryName)
		}
		obj, diags := types.ObjectValue(objType.AttrTypes, vals)
		resp.Diagnostics.Append(diags...)
		elems = append(elems, obj)
	}
	rules, diags := types.ListValue(objType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), networkID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rules"), rules)...)
}

// cidrValidator rejects what the API would refuse at apply time.
type cidrValidator struct{}

func (cidrValidator) Description(context.Context) string { return "value must be a CIDR range" }

func (v cidrValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (cidrValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, _, err := net.ParseCIDR(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid CIDR",
			fmt.Sprintf("%q is not a CIDR range; write a single address as %s/32 (IPv4) or /128 (IPv6).",
				req.ConfigValue.ValueString(), req.ConfigValue.ValueString()))
	}
}
