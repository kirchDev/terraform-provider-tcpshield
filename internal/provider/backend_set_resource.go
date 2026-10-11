package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

var (
	_ resource.Resource                = (*backendSetResource)(nil)
	_ resource.ResourceWithImportState = (*backendSetResource)(nil)
)

// NewBackendSetResource manages a backend set — the pool of origin servers a
// domain or Bedrock tunnel forwards to.
func NewBackendSetResource() resource.Resource { return &backendSetResource{} }

type backendSetResource struct{ resourceBase }

type apiBackendSet struct {
	ID                int64      `json:"id"`
	Name              string     `json:"name"`
	ProxyProtocol     bool       `json:"proxy_protocol"`
	VulcanACEnabled   bool       `json:"vulcan_ac_enabled"`
	LoadBalancingMode int64      `json:"load_balancing_mode"`
	Backends          *[]string  `json:"backends"`
	CreatedAt         flexString `json:"created_at"`
}

type backendSetModel struct {
	NetworkID         types.Int64  `tfsdk:"network_id"`
	ID                types.Int64  `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Backends          types.List   `tfsdk:"backends"`
	ProxyProtocol     types.Bool   `tfsdk:"proxy_protocol"`
	VulcanACEnabled   types.Bool   `tfsdk:"vulcan_ac_enabled"`
	LoadBalancingMode types.Int64  `tfsdk:"load_balancing_mode"`
	CreatedAt         types.String `tfsdk:"created_at"`
}

// fill copies b into m. id is the entity's id from the request path: the
// single-entity read carries backends but no id, the listing the reverse.
func (m *backendSetModel) fill(networkID, id int64, b apiBackendSet) {
	m.NetworkID = types.Int64Value(networkID)
	m.ID = types.Int64Value(id)
	m.Name = types.StringValue(b.Name)
	m.ProxyProtocol = types.BoolValue(b.ProxyProtocol)
	m.VulcanACEnabled = types.BoolValue(b.VulcanACEnabled)
	m.LoadBalancingMode = types.Int64Value(b.LoadBalancingMode)
	m.CreatedAt = b.CreatedAt.value()
	// Only the single-entity read carries backends. When the response has them
	// they are authoritative; when it has none the configured list stands.
	if b.Backends != nil {
		m.Backends = stringList(*b.Backends)
	} else if m.Backends.IsNull() || m.Backends.IsUnknown() {
		m.Backends = stringList(nil)
	}
}

// body is the whole backend set. The PATCH behaves like a PUT — a field left
// out falls back to its zero value (load_balancing_mode resets to 0), and a body
// without backends is answered "Backend set is identical, no update necessary"
// and ignored — so create and update both send everything. Unknown
// Optional+Computed values (create without them) are left to the API default.
func (m *backendSetModel) body(ctx context.Context, diags *diagsT) body {
	b := body{"name": m.Name.ValueString()}
	b.putStringList(ctx, "backends", m.Backends, types.ListNull(types.StringType), diags)
	b.putBool("proxy_protocol", m.ProxyProtocol, types.BoolNull())
	b.putBool("vulcan_ac_enabled", m.VulcanACEnabled, types.BoolNull())
	b.putInt64("load_balancing_mode", m.LoadBalancingMode, types.Int64Null())
	return b
}

func (r *backendSetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backend_set"
}

func (r *backendSetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A backend set: the origin servers (`host:port`) a domain or Bedrock tunnel forwards traffic to.",
		Attributes: map[string]schema.Attribute{
			"network_id": parentIDAttr("Network the backend set belongs to."),
			"id":         idAttr("Numeric backend set id."),
			"name": schema.StringAttribute{
				MarkdownDescription: "Backend set name.",
				Required:            true,
			},
			"backends": schema.ListAttribute{
				MarkdownDescription: "Origin servers as `host:port`, e.g. `203.0.113.10:25565`.",
				Required:            true,
				ElementType:         types.StringType,
			},
			"proxy_protocol":      optBool("Send the PROXY protocol header to the backends."),
			"vulcan_ac_enabled":   optBool("Enable the Vulcan anti-cheat integration."),
			"load_balancing_mode": optInt("Load balancing mode, as the panel's numeric code."),
			"created_at":          computedStr("Creation timestamp."),
		},
	}
}

func backendSetPath(networkID, id int64) string {
	return networkPath(networkID, fmt.Sprintf("/backendSets/%d", id))
}

func (r *backendSetResource) read(ctx context.Context, networkID, id int64) (apiBackendSet, error) {
	var b apiBackendSet
	raw, err := r.client.Do(ctx, http.MethodGet, backendSetPath(networkID, id), nil)
	if err == nil {
		err = client.DecodeOne(raw, &b)
	}
	return b, err
}

func (r *backendSetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan backendSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID := plan.NetworkID.ValueInt64()
	b := plan.body(ctx, &resp.Diagnostics)
	raw, err := r.client.Write(ctx, http.MethodPost, networkPath(networkID, "/backendSets"), b, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating backend set", err.Error())
		return
	}
	id, ok := client.CreatedID(raw)
	if !ok {
		id, err = findNewest(ctx, r.client, networkPath(networkID, "/backendSets"), func(x apiBackendSet) (int64, bool) {
			return x.ID, x.Name == plan.Name.ValueString()
		})
		if err != nil {
			resp.Diagnostics.AddError("Error locating created backend set", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), networkID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)

	got, err := r.read(ctx, networkID, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading created backend set", err.Error())
		return
	}
	plan.fill(networkID, id, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *backendSetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state backendSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.read(ctx, state.NetworkID.ValueInt64(), state.ID.ValueInt64())
	if client.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading backend set", err.Error())
		return
	}
	state.fill(state.NetworkID.ValueInt64(), state.ID.ValueInt64(), got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *backendSetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state backendSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID, id := state.NetworkID.ValueInt64(), state.ID.ValueInt64()
	if _, err := r.client.Write(ctx, http.MethodPatch, backendSetPath(networkID, id), plan.body(ctx, &resp.Diagnostics), nil); err != nil {
		resp.Diagnostics.AddError("Error updating backend set", err.Error())
		return
	}
	got, err := r.read(ctx, networkID, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading updated backend set", err.Error())
		return
	}
	plan.fill(networkID, id, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *backendSetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state backendSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, backendSetPath(state.NetworkID.ValueInt64(), state.ID.ValueInt64())); err != nil {
		resp.Diagnostics.AddError("Error deleting backend set", err.Error())
	}
}

func (r *backendSetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportID(req.ID, "network_id", "backend_set_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[1])...)
}

// --- data sources ---

func backendSetDSAttrs() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"network_id":          dsInt("Network the backend set belongs to."),
		"id":                  dsInt("Numeric backend set id."),
		"name":                dsStr("Backend set name."),
		"backends":            dsStrList("Origin servers as `host:port`."),
		"proxy_protocol":      dsBool("Whether the PROXY protocol header is sent to the backends."),
		"vulcan_ac_enabled":   dsBool("Whether the Vulcan anti-cheat integration is enabled."),
		"load_balancing_mode": dsInt("Load balancing mode, as the panel's numeric code."),
		"created_at":          dsStr("Creation timestamp."),
	}
}

// NewBackendSetDataSource reads one backend set.
func NewBackendSetDataSource() datasource.DataSource { return &backendSetDataSource{} }

type backendSetDataSource struct{ dataSourceBase }

func (d *backendSetDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backend_set"
}

func (d *backendSetDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Reads a single backend set of a network.",
		Attributes: with(backendSetDSAttrs(), map[string]dschema.Attribute{
			"network_id": dsRequiredInt("Network the backend set belongs to."),
			"id":         dsRequiredInt("Numeric backend set id."),
		}),
	}
}

func (d *backendSetDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg backendSetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var b apiBackendSet
	raw, err := d.client.Do(ctx, http.MethodGet, backendSetPath(cfg.NetworkID.ValueInt64(), cfg.ID.ValueInt64()), nil)
	if err == nil {
		err = client.DecodeOne(raw, &b)
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading backend set", err.Error())
		return
	}
	cfg.fill(cfg.NetworkID.ValueInt64(), cfg.ID.ValueInt64(), b)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewBackendSetsDataSource lists a network's backend sets.
func NewBackendSetsDataSource() datasource.DataSource { return &backendSetsDataSource{} }

type backendSetsDataSource struct{ dataSourceBase }

type backendSetsModel struct {
	NetworkID   types.Int64       `tfsdk:"network_id"`
	BackendSets []backendSetModel `tfsdk:"backend_sets"`
}

func (d *backendSetsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backend_sets"
}

func (d *backendSetsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Lists the backend sets of a network.",
		Attributes: map[string]dschema.Attribute{
			"network_id":   dsRequiredInt("Network to list."),
			"backend_sets": dsList("The backend sets.", backendSetDSAttrs()),
		},
	}
}

func (d *backendSetsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg backendSetsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []apiBackendSet
	if err := d.client.Get(ctx, networkPath(cfg.NetworkID.ValueInt64(), "/backendSets"), &list); err != nil {
		resp.Diagnostics.AddError("Error listing backend sets", err.Error())
		return
	}
	// The listing carries no backends, so each set is read once more for them.
	networkID := cfg.NetworkID.ValueInt64()
	cfg.BackendSets = make([]backendSetModel, len(list))
	for i, b := range list {
		var full apiBackendSet
		raw, err := d.client.Do(ctx, http.MethodGet, backendSetPath(networkID, b.ID), nil)
		if err == nil {
			err = client.DecodeOne(raw, &full)
		}
		if err != nil {
			resp.Diagnostics.AddError("Error reading backend set", err.Error())
			return
		}
		cfg.BackendSets[i].fill(networkID, b.ID, full)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
