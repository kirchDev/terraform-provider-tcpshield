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
	_ resource.Resource                = (*bedrockTunnelResource)(nil)
	_ resource.ResourceWithImportState = (*bedrockTunnelResource)(nil)
)

// NewBedrockTunnelResource manages a Bedrock (UDP) tunnel of a network.
func NewBedrockTunnelResource() resource.Resource { return &bedrockTunnelResource{} }

type bedrockTunnelResource struct{ resourceBase }

type apiBedrockTunnel struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	BackendSetID   int64      `json:"backend_set_id"`
	Location       string     `json:"location"`
	ProxyProtocol  int64      `json:"proxy_protocol"`
	AllocatedCname string     `json:"allocated_cname"`
	CreatedAt      flexString `json:"created_at"`
}

type bedrockTunnelModel struct {
	NetworkID      types.Int64  `tfsdk:"network_id"`
	ID             types.Int64  `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	BackendSetID   types.Int64  `tfsdk:"backend_set_id"`
	Location       types.String `tfsdk:"location"`
	ProxyProtocol  types.Int64  `tfsdk:"proxy_protocol"`
	AllocatedCname types.String `tfsdk:"allocated_cname"`
	CreatedAt      types.String `tfsdk:"created_at"`
}

func (m *bedrockTunnelModel) fill(networkID int64, t apiBedrockTunnel) {
	m.NetworkID = types.Int64Value(networkID)
	m.ID = types.Int64Value(t.ID)
	m.Name = types.StringValue(t.Name)
	m.BackendSetID = types.Int64Value(t.BackendSetID)
	m.Location = types.StringValue(t.Location)
	m.ProxyProtocol = types.Int64Value(t.ProxyProtocol)
	m.AllocatedCname = types.StringValue(t.AllocatedCname)
	m.CreatedAt = t.CreatedAt.value()
}

// body is the full CreateBedrockTunnelsRequest — the PATCH takes the same
// schema, so it is sent whole on both paths.
func (m *bedrockTunnelModel) body() body {
	b := body{
		"name":           m.Name.ValueString(),
		"backend_set_id": m.BackendSetID.ValueInt64(),
		"location":       m.Location.ValueString(),
	}
	b.putInt64("proxy_protocol", m.ProxyProtocol, types.Int64Null())
	return b
}

func (r *bedrockTunnelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bedrock_tunnel"
}

func (r *bedrockTunnelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Bedrock tunnel: protected UDP ingress for Minecraft Bedrock Edition, forwarding to a backend set. " +
			"Players connect to `allocated_cname`. Needs a plan with Geyser support (Premium or Enterprise) — on Pro the API answers " +
			"`You cannot add any more bedrock tunnels.` **Not yet verified against the live API.**",
		Attributes: map[string]schema.Attribute{
			"network_id": parentIDAttr("Network the tunnel belongs to."),
			"id":         idAttr("Numeric tunnel id."),
			"name": schema.StringAttribute{
				MarkdownDescription: "Tunnel name.",
				Required:            true,
			},
			"backend_set_id": schema.Int64Attribute{
				MarkdownDescription: "Backend set the tunnel forwards to.",
				Required:            true,
			},
			"location": schema.StringAttribute{
				MarkdownDescription: "Ingress location, one of `tcpshield_bedrock_tunnel_locations` (e.g. `FRA`, `ANY`).",
				Required:            true,
			},
			"proxy_protocol":  optInt("PROXY protocol mode sent to the backends, as the panel's numeric code (`0` = off)."),
			"allocated_cname": computedStr("Hostname players connect to."),
			"created_at":      computedStr("Creation timestamp."),
		},
	}
}

func bedrockTunnelPath(networkID, id int64) string {
	return networkPath(networkID, fmt.Sprintf("/bedrockTunnels/%d", id))
}

func readBedrockTunnel(ctx context.Context, c *client.Client, networkID, id int64) (apiBedrockTunnel, error) {
	var t apiBedrockTunnel
	raw, err := c.Do(ctx, http.MethodGet, bedrockTunnelPath(networkID, id), nil)
	if err == nil {
		err = client.DecodeOne(raw, &t)
	}
	return t, err
}

func (r *bedrockTunnelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan bedrockTunnelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID := plan.NetworkID.ValueInt64()
	raw, err := r.client.Write(ctx, http.MethodPost, networkPath(networkID, "/bedrockTunnels"), plan.body(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Bedrock tunnel", err.Error())
		return
	}
	id, ok := client.CreatedID(raw)
	if !ok {
		id, err = findNewest(ctx, r.client, networkPath(networkID, "/bedrockTunnels"), func(t apiBedrockTunnel) (int64, bool) {
			return t.ID, t.Name == plan.Name.ValueString()
		})
		if err != nil {
			resp.Diagnostics.AddError("Error locating created Bedrock tunnel", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), networkID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)

	got, err := readBedrockTunnel(ctx, r.client, networkID, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading created Bedrock tunnel", err.Error())
		return
	}
	plan.fill(networkID, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bedrockTunnelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state bedrockTunnelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := readBedrockTunnel(ctx, r.client, state.NetworkID.ValueInt64(), state.ID.ValueInt64())
	if client.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading Bedrock tunnel", err.Error())
		return
	}
	state.fill(state.NetworkID.ValueInt64(), got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *bedrockTunnelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state bedrockTunnelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID, id := state.NetworkID.ValueInt64(), state.ID.ValueInt64()
	if _, err := r.client.Write(ctx, http.MethodPatch, bedrockTunnelPath(networkID, id), plan.body(), nil); err != nil {
		resp.Diagnostics.AddError("Error updating Bedrock tunnel", err.Error())
		return
	}
	got, err := readBedrockTunnel(ctx, r.client, networkID, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading updated Bedrock tunnel", err.Error())
		return
	}
	plan.fill(networkID, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *bedrockTunnelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state bedrockTunnelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, bedrockTunnelPath(state.NetworkID.ValueInt64(), state.ID.ValueInt64())); err != nil {
		resp.Diagnostics.AddError("Error deleting Bedrock tunnel", err.Error())
	}
}

func (r *bedrockTunnelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportID(req.ID, "network_id", "tunnel_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[1])...)
}

// --- data sources ---

func bedrockTunnelDSAttrs() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"network_id":      dsInt("Network the tunnel belongs to."),
		"id":              dsInt("Numeric tunnel id."),
		"name":            dsStr("Tunnel name."),
		"backend_set_id":  dsInt("Backend set the tunnel forwards to."),
		"location":        dsStr("Ingress location."),
		"proxy_protocol":  dsInt("PROXY protocol mode, as the panel's numeric code."),
		"allocated_cname": dsStr("Hostname players connect to."),
		"created_at":      dsStr("Creation timestamp."),
	}
}

// NewBedrockTunnelDataSource reads one Bedrock tunnel.
func NewBedrockTunnelDataSource() datasource.DataSource { return &bedrockTunnelDataSource{} }

type bedrockTunnelDataSource struct{ dataSourceBase }

func (d *bedrockTunnelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bedrock_tunnel"
}

func (d *bedrockTunnelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Reads a single Bedrock tunnel of a network.",
		Attributes: with(bedrockTunnelDSAttrs(), map[string]dschema.Attribute{
			"network_id": dsRequiredInt("Network the tunnel belongs to."),
			"id":         dsRequiredInt("Numeric tunnel id."),
		}),
	}
}

func (d *bedrockTunnelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg bedrockTunnelModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := readBedrockTunnel(ctx, d.client, cfg.NetworkID.ValueInt64(), cfg.ID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Error reading Bedrock tunnel", err.Error())
		return
	}
	cfg.fill(cfg.NetworkID.ValueInt64(), got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewBedrockTunnelsDataSource lists a network's Bedrock tunnels.
func NewBedrockTunnelsDataSource() datasource.DataSource { return &bedrockTunnelsDataSource{} }

type bedrockTunnelsDataSource struct{ dataSourceBase }

type bedrockTunnelsModel struct {
	NetworkID types.Int64          `tfsdk:"network_id"`
	Tunnels   []bedrockTunnelModel `tfsdk:"tunnels"`
}

func (d *bedrockTunnelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bedrock_tunnels"
}

func (d *bedrockTunnelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Lists the Bedrock tunnels of a network.",
		Attributes: map[string]dschema.Attribute{
			"network_id": dsRequiredInt("Network to list."),
			"tunnels":    dsList("The tunnels.", bedrockTunnelDSAttrs()),
		},
	}
}

func (d *bedrockTunnelsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg bedrockTunnelsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []apiBedrockTunnel
	if err := d.client.Get(ctx, networkPath(cfg.NetworkID.ValueInt64(), "/bedrockTunnels"), &list); err != nil {
		resp.Diagnostics.AddError("Error listing Bedrock tunnels", err.Error())
		return
	}
	cfg.Tunnels = make([]bedrockTunnelModel, len(list))
	for i, t := range list {
		cfg.Tunnels[i].fill(cfg.NetworkID.ValueInt64(), t)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewBedrockTunnelLocationsDataSource lists the locations a Bedrock tunnel can use.
func NewBedrockTunnelLocationsDataSource() datasource.DataSource {
	return &bedrockTunnelLocationsDataSource{}
}

type bedrockTunnelLocationsDataSource struct{ dataSourceBase }

type bedrockTunnelLocationsModel struct {
	NetworkID types.Int64 `tfsdk:"network_id"`
	Locations types.List  `tfsdk:"locations"`
}

func (d *bedrockTunnelLocationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bedrock_tunnel_locations"
}

func (d *bedrockTunnelLocationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Lists the locations a Bedrock tunnel of the network can be placed in (e.g. `FRA`, `IAD`, `ANY`).",
		Attributes: map[string]dschema.Attribute{
			"network_id": dsRequiredInt("Network to list locations for."),
			"locations":  dsStrList("Location codes."),
		},
	}
}

func (d *bedrockTunnelLocationsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg bedrockTunnelLocationsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []string
	if err := d.client.Get(ctx, networkPath(cfg.NetworkID.ValueInt64(), "/bedrockTunnels/locations"), &list); err != nil {
		resp.Diagnostics.AddError("Error listing Bedrock tunnel locations", err.Error())
		return
	}
	cfg.Locations = stringList(list)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
