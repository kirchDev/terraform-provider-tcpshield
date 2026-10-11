package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

var (
	_ resource.Resource                = (*sentryTunnelResource)(nil)
	_ resource.ResourceWithImportState = (*sentryTunnelResource)(nil)
)

// NewSentryTunnelResource manages a Sentry tunnel — an account-level WireGuard
// tunnel that fronts any TCP/UDP service, not tied to a network.
func NewSentryTunnelResource() resource.Resource { return &sentryTunnelResource{} }

type sentryTunnelResource struct{ resourceBase }

type apiTunnelAddress struct {
	IPAddressPublic      string `json:"ip_address_public"`
	IPAddressPrivate     string `json:"ip_address_private"`
	PeerIPAddressPrivate string `json:"peer_ip_address_private"`
	Location             string `json:"location"`
}

type apiSentryTunnel struct {
	ID                       int64              `json:"id"`
	Name                     string             `json:"name"`
	EndpointPublicKey        string             `json:"endpoint_public_key"`
	EndpointPrivateKey       string             `json:"endpoint_private_key"`
	EndpointPeer             string             `json:"endpoint_peer"`
	BindPort                 int64              `json:"bind_port"`
	Location                 string             `json:"location"`
	CurrentBandwidthUsage    int64              `json:"current_bandwidth_usage"`
	MonthBandwidthUsage      int64              `json:"month_bandwidth_usage"`
	CumulativeBandwidthUsage int64              `json:"cumulative_bandwidth_usage"`
	Ingress                  int64              `json:"ingress"`
	Egress                   int64              `json:"egress"`
	TunnelAddresses          []apiTunnelAddress `json:"tunnel_addresses"`
	CreatedAt                flexString         `json:"created_at"`
}

var tunnelAddressType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"ip_address_public":       types.StringType,
	"ip_address_private":      types.StringType,
	"peer_ip_address_private": types.StringType,
	"location":                types.StringType,
}}

func tunnelAddresses(in []apiTunnelAddress, diags *diag.Diagnostics) types.List {
	elems := make([]attr.Value, len(in))
	for i, a := range in {
		obj, d := types.ObjectValue(tunnelAddressType.AttrTypes, map[string]attr.Value{
			"ip_address_public":       types.StringValue(a.IPAddressPublic),
			"ip_address_private":      types.StringValue(a.IPAddressPrivate),
			"peer_ip_address_private": types.StringValue(a.PeerIPAddressPrivate),
			"location":                types.StringValue(a.Location),
		})
		diags.Append(d...)
		elems[i] = obj
	}
	list, d := types.ListValue(tunnelAddressType, elems)
	diags.Append(d...)
	return list
}

type sentryTunnelModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	EndpointPeer       types.String `tfsdk:"endpoint_peer"`
	BindPort           types.Int64  `tfsdk:"bind_port"`
	Location           types.String `tfsdk:"location"`
	EndpointPublicKey  types.String `tfsdk:"endpoint_public_key"`
	EndpointPrivateKey types.String `tfsdk:"endpoint_private_key"`
	TunnelAddresses    types.List   `tfsdk:"tunnel_addresses"`
	CreatedAt          types.String `tfsdk:"created_at"`
}

func (m *sentryTunnelModel) fill(t apiSentryTunnel, diags *diag.Diagnostics) {
	m.ID = types.Int64Value(t.ID)
	m.Name = types.StringValue(t.Name)
	m.EndpointPeer = types.StringValue(t.EndpointPeer)
	m.BindPort = types.Int64Value(t.BindPort)
	m.Location = types.StringValue(t.Location)
	m.EndpointPublicKey = types.StringValue(t.EndpointPublicKey)
	m.EndpointPrivateKey = types.StringValue(t.EndpointPrivateKey)
	m.TunnelAddresses = tunnelAddresses(t.TunnelAddresses, diags)
	m.CreatedAt = t.CreatedAt.value()
}

func (r *sentryTunnelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel"
}

func (r *sentryTunnelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Sentry tunnel: an account-level WireGuard tunnel that puts TCPShield's filtering in front of any TCP/UDP service. " +
			"Install it on the origin with `tcpshield_sentry_tunnel_setup_script`, then open ports with `tcpshield_sentry_tunnel_firewall_rule`. " +
			"**Not yet verified against the live API** — creation failed server-side on the test account (`Allocating not used IP address failed.`).",
		Attributes: map[string]schema.Attribute{
			"id": idAttr("Numeric tunnel id."),
			"name": schema.StringAttribute{
				MarkdownDescription: "Tunnel name, 3–50 characters.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(3, 50)},
			},
			"endpoint_peer": schema.StringAttribute{
				MarkdownDescription: "Public address of the origin's WireGuard peer.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtMost(128)},
			},
			"bind_port": schema.Int64Attribute{
				MarkdownDescription: "Port the tunnel binds on TCPShield's side. `tcpshield_sentry_tunnel_bind_port` suggests a free one.",
				Required:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
			},
			"location": schema.StringAttribute{
				MarkdownDescription: "Location key from `tcpshield_sentry_tunnel_locations` (e.g. `ANY`). Only settable on create — changing it replaces the tunnel.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"endpoint_public_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard public key of the origin's endpoint. TCPShield generates one on create; set it to bring your own key.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtMost(64)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"endpoint_private_key": schema.StringAttribute{
				MarkdownDescription: "WireGuard private key TCPShield generated for the origin's endpoint.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tunnel_addresses": schema.ListNestedAttribute{
				MarkdownDescription: "Addresses allocated to the tunnel.",
				Computed:            true,
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"ip_address_public":       schema.StringAttribute{Computed: true, MarkdownDescription: "Public IP clients connect to."},
					"ip_address_private":      schema.StringAttribute{Computed: true, MarkdownDescription: "Private tunnel IP on TCPShield's side."},
					"peer_ip_address_private": schema.StringAttribute{Computed: true, MarkdownDescription: "Private tunnel IP of the origin."},
					"location":                schema.StringAttribute{Computed: true, MarkdownDescription: "Location of the address."},
				}},
			},
			"created_at": computedStr("Creation timestamp."),
		},
	}
}

func sentryTunnelPath(id int64) string { return fmt.Sprintf("/tunnels/%d", id) }

func readSentryTunnel(ctx context.Context, c *client.Client, id int64) (apiSentryTunnel, error) {
	var t apiSentryTunnel
	raw, err := c.Do(ctx, http.MethodGet, sentryTunnelPath(id), nil)
	if err == nil {
		err = client.DecodeOne(raw, &t)
	}
	return t, err
}

func (r *sentryTunnelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sentryTunnelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	b := body{
		"name":          plan.Name.ValueString(),
		"endpoint_peer": plan.EndpointPeer.ValueString(),
		"bind_port":     plan.BindPort.ValueInt64(),
	}
	b.putString("location", plan.Location, types.StringNull())
	raw, err := r.client.Write(ctx, http.MethodPost, "/tunnels", b, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Sentry tunnel", err.Error())
		return
	}
	id, ok := client.CreatedID(raw)
	if !ok {
		id, err = findNewest(ctx, r.client, "/tunnels", func(t apiSentryTunnel) (int64, bool) {
			return t.ID, t.Name == plan.Name.ValueString()
		})
		if err != nil {
			resp.Diagnostics.AddError("Error locating created Sentry tunnel", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)

	got, err := readSentryTunnel(ctx, r.client, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading created Sentry tunnel", err.Error())
		return
	}
	// A configured public key the create body could not carry goes through
	// the update, which is the only call that accepts one.
	if !plan.EndpointPublicKey.IsUnknown() && plan.EndpointPublicKey.ValueString() != got.EndpointPublicKey {
		if got, err = r.put(ctx, id, &plan, got.EndpointPublicKey); err != nil {
			resp.Diagnostics.AddError("Error setting Sentry tunnel public key", err.Error())
			return
		}
	}
	plan.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// put sends the full UpdateSentryTunnelRequest — the API requires all four
// fields on every update — and reads the tunnel back.
func (r *sentryTunnelResource) put(ctx context.Context, id int64, plan *sentryTunnelModel, currentKey string) (apiSentryTunnel, error) {
	key := currentKey
	if !plan.EndpointPublicKey.IsUnknown() && !plan.EndpointPublicKey.IsNull() {
		key = plan.EndpointPublicKey.ValueString()
	}
	b := body{
		"name":                plan.Name.ValueString(),
		"endpoint_public_key": key,
		"endpoint_peer":       plan.EndpointPeer.ValueString(),
		"bind_port":           plan.BindPort.ValueInt64(),
	}
	if _, err := r.client.Write(ctx, http.MethodPut, sentryTunnelPath(id), b, nil); err != nil {
		return apiSentryTunnel{}, err
	}
	return readSentryTunnel(ctx, r.client, id)
}

func (r *sentryTunnelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sentryTunnelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := readSentryTunnel(ctx, r.client, state.ID.ValueInt64())
	if client.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading Sentry tunnel", err.Error())
		return
	}
	state.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sentryTunnelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sentryTunnelModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.put(ctx, state.ID.ValueInt64(), &plan, state.EndpointPublicKey.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error updating Sentry tunnel", err.Error())
		return
	}
	plan.fill(got, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sentryTunnelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sentryTunnelModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, sentryTunnelPath(state.ID.ValueInt64())); err != nil {
		resp.Diagnostics.AddError("Error deleting Sentry tunnel", err.Error())
	}
}

func (r *sentryTunnelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportID(req.ID, "tunnel_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[0])...)
}
