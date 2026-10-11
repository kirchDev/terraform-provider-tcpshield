package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func tunnelAddressDSAttr() schema.ListNestedAttribute {
	return dsList("Addresses allocated to the tunnel.", map[string]schema.Attribute{
		"ip_address_public":       dsStr("Public IP clients connect to."),
		"ip_address_private":      dsStr("Private tunnel IP on TCPShield's side."),
		"peer_ip_address_private": dsStr("Private tunnel IP of the origin."),
		"location":                dsStr("Location of the address."),
	})
}

func sentryTunnelDSAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":                         dsInt("Numeric tunnel id."),
		"name":                       dsStr("Tunnel name."),
		"endpoint_peer":              dsStr("Public address of the origin's WireGuard peer."),
		"bind_port":                  dsInt("Port the tunnel binds on TCPShield's side."),
		"location":                   dsStr("Location key."),
		"endpoint_public_key":        dsStr("WireGuard public key of the origin's endpoint."),
		"endpoint_private_key":       dsSensitiveStr("WireGuard private key generated for the origin's endpoint."),
		"tunnel_addresses":           tunnelAddressDSAttr(),
		"current_bandwidth_usage":    dsInt("Current bandwidth usage in bytes."),
		"month_bandwidth_usage":      dsInt("Bandwidth used this month in bytes."),
		"cumulative_bandwidth_usage": dsInt("Total bandwidth used in bytes."),
		"ingress":                    dsInt("Ingress in bytes."),
		"egress":                     dsInt("Egress in bytes."),
		"created_at":                 dsStr("Creation timestamp."),
	}
}

type sentryTunnelDSModel struct {
	ID                       types.Int64  `tfsdk:"id"`
	Name                     types.String `tfsdk:"name"`
	EndpointPeer             types.String `tfsdk:"endpoint_peer"`
	BindPort                 types.Int64  `tfsdk:"bind_port"`
	Location                 types.String `tfsdk:"location"`
	EndpointPublicKey        types.String `tfsdk:"endpoint_public_key"`
	EndpointPrivateKey       types.String `tfsdk:"endpoint_private_key"`
	TunnelAddresses          types.List   `tfsdk:"tunnel_addresses"`
	CurrentBandwidthUsage    types.Int64  `tfsdk:"current_bandwidth_usage"`
	MonthBandwidthUsage      types.Int64  `tfsdk:"month_bandwidth_usage"`
	CumulativeBandwidthUsage types.Int64  `tfsdk:"cumulative_bandwidth_usage"`
	Ingress                  types.Int64  `tfsdk:"ingress"`
	Egress                   types.Int64  `tfsdk:"egress"`
	CreatedAt                types.String `tfsdk:"created_at"`
}

func (m *sentryTunnelDSModel) fill(t apiSentryTunnel, diags *diagsT) {
	var base sentryTunnelModel
	base.fill(t, diags)
	m.ID, m.Name, m.EndpointPeer, m.BindPort = base.ID, base.Name, base.EndpointPeer, base.BindPort
	m.Location, m.EndpointPublicKey, m.EndpointPrivateKey = base.Location, base.EndpointPublicKey, base.EndpointPrivateKey
	m.TunnelAddresses, m.CreatedAt = base.TunnelAddresses, base.CreatedAt
	m.CurrentBandwidthUsage = types.Int64Value(t.CurrentBandwidthUsage)
	m.MonthBandwidthUsage = types.Int64Value(t.MonthBandwidthUsage)
	m.CumulativeBandwidthUsage = types.Int64Value(t.CumulativeBandwidthUsage)
	m.Ingress = types.Int64Value(t.Ingress)
	m.Egress = types.Int64Value(t.Egress)
}

// NewSentryTunnelDataSource reads one Sentry tunnel, usage counters included.
func NewSentryTunnelDataSource() datasource.DataSource { return &sentryTunnelDataSource{} }

type sentryTunnelDataSource struct{ dataSourceBase }

func (d *sentryTunnelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel"
}

func (d *sentryTunnelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a single Sentry tunnel, including its bandwidth counters.",
		Attributes:          with(sentryTunnelDSAttrs(), map[string]schema.Attribute{"id": dsRequiredInt("Numeric tunnel id.")}),
	}
}

func (d *sentryTunnelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sentryTunnelDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	t, err := readSentryTunnel(ctx, d.client, cfg.ID.ValueInt64())
	if err != nil {
		resp.Diagnostics.AddError("Error reading Sentry tunnel", err.Error())
		return
	}
	cfg.fill(t, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewSentryTunnelsDataSource lists every Sentry tunnel on the account.
func NewSentryTunnelsDataSource() datasource.DataSource { return &sentryTunnelsDataSource{} }

type sentryTunnelsDataSource struct{ dataSourceBase }

type sentryTunnelsModel struct {
	Tunnels []sentryTunnelDSModel `tfsdk:"tunnels"`
}

func (d *sentryTunnelsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnels"
}

func (d *sentryTunnelsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every Sentry tunnel on the account.",
		Attributes:          map[string]schema.Attribute{"tunnels": dsList("The tunnels.", sentryTunnelDSAttrs())},
	}
}

func (d *sentryTunnelsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var list []apiSentryTunnel
	if err := d.client.Get(ctx, "/tunnels", &list); err != nil {
		resp.Diagnostics.AddError("Error listing Sentry tunnels", err.Error())
		return
	}
	out := sentryTunnelsModel{Tunnels: make([]sentryTunnelDSModel, len(list))}
	for i, t := range list {
		out.Tunnels[i].fill(t, &resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// NewSentryTunnelLocationsDataSource lists the Sentry tunnel locations.
func NewSentryTunnelLocationsDataSource() datasource.DataSource {
	return &sentryTunnelLocationsDataSource{}
}

type sentryTunnelLocationsDataSource struct{ dataSourceBase }

type sentryTunnelLocationsModel struct {
	Locations types.Map  `tfsdk:"locations"`
	Keys      types.List `tfsdk:"keys"`
}

func (d *sentryTunnelLocationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel_locations"
}

func (d *sentryTunnelLocationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the locations a Sentry tunnel can be placed in.",
		Attributes: map[string]schema.Attribute{
			"locations": schema.MapAttribute{
				MarkdownDescription: "Location key → display name, e.g. `ANY = \"Anycast\"`.",
				Computed:            true,
				ElementType:         types.StringType,
			},
			"keys": dsStrList("The location keys, sorted."),
		},
	}
}

func (d *sentryTunnelLocationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m map[string]string
	raw, err := d.client.Do(ctx, http.MethodGet, "/tunnels/locations", nil)
	if err == nil {
		err = decodeLocations(raw, &m)
	}
	if err != nil {
		resp.Diagnostics.AddError("Error listing Sentry tunnel locations", err.Error())
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	locs, diags := types.MapValueFrom(ctx, types.StringType, m)
	resp.Diagnostics.Append(diags...)
	out := sentryTunnelLocationsModel{Locations: locs, Keys: stringList(keys)}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// decodeLocations accepts the live shape (an object) and the documented one (an
// array of such objects), merging the latter.
func decodeLocations(raw json.RawMessage, out *map[string]string) error {
	*out = map[string]string{}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var list []map[string]string
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
		for _, m := range list {
			for k, v := range m {
				(*out)[k] = v
			}
		}
		return nil
	}
	return json.Unmarshal(raw, out)
}

// NewSentryTunnelBindPortDataSource asks TCPShield for a free bind port.
func NewSentryTunnelBindPortDataSource() datasource.DataSource {
	return &sentryTunnelBindPortDataSource{}
}

type sentryTunnelBindPortDataSource struct{ dataSourceBase }

type sentryTunnelBindPortModel struct {
	Port types.Int64 `tfsdk:"port"`
}

func (d *sentryTunnelBindPortDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel_bind_port"
}

func (d *sentryTunnelBindPortDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Asks TCPShield for a free Sentry tunnel bind port. The answer changes on every read, so feeding it " +
			"straight into `tcpshield_sentry_tunnel.bind_port` needs `lifecycle { ignore_changes = [bind_port] }` — " +
			"otherwise every plan proposes a new port.",
		Attributes: map[string]schema.Attribute{"port": dsInt("A currently free bind port.")},
	}
}

func (d *sentryTunnelBindPortDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	raw, err := d.client.Do(ctx, http.MethodGet, "/tunnels/getBindPort", nil)
	if err != nil {
		resp.Diagnostics.AddError("Error fetching bind port", err.Error())
		return
	}
	port, err := strconv.ParseInt(strings.Trim(strings.TrimSpace(string(raw)), `"`), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected bind port response", fmt.Sprintf("%q is not a port: %v", raw, err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &sentryTunnelBindPortModel{Port: types.Int64Value(port)})...)
}

// NewSentryTunnelSetupScriptDataSource returns the install script of a tunnel.
func NewSentryTunnelSetupScriptDataSource() datasource.DataSource {
	return &sentryTunnelSetupScriptDataSource{}
}

type sentryTunnelSetupScriptDataSource struct{ dataSourceBase }

type sentryTunnelSetupScriptModel struct {
	TunnelID types.Int64  `tfsdk:"tunnel_id"`
	Script   types.String `tfsdk:"script"`
}

func (d *sentryTunnelSetupScriptDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel_setup_script"
}

func (d *sentryTunnelSetupScriptDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The shell script that installs a Sentry tunnel's WireGuard endpoint on the origin — e.g. for cloud-init `user_data`. " +
			"It embeds the endpoint's private key, so it is marked sensitive.",
		Attributes: map[string]schema.Attribute{
			"tunnel_id": dsRequiredInt("Sentry tunnel id."),
			"script":    dsSensitiveStr("The setup script."),
		},
	}
}

func (d *sentryTunnelSetupScriptDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sentryTunnelSetupScriptModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := d.client.Do(ctx, http.MethodGet, fmt.Sprintf("/tunnels/%d/setupScript", cfg.TunnelID.ValueInt64()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error fetching setup script", err.Error())
		return
	}
	cfg.Script = types.StringValue(setupScript(raw))
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// setupScript unwraps {"data": "<script>"} (also inside a one-element array);
// anything else is taken as the script itself.
func setupScript(raw json.RawMessage) string {
	var wrapped struct {
		Data string `json:"data"`
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) == nil && len(list) == 1 {
			raw = list[0]
		}
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Data != "" {
		return wrapped.Data
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return trimmed
}

// NewSentryTunnelAnalyticsDataSource reads a tunnel's bandwidth time series.
func NewSentryTunnelAnalyticsDataSource() datasource.DataSource {
	return &sentryTunnelAnalyticsDataSource{}
}

type sentryTunnelAnalyticsDataSource struct{ dataSourceBase }

type sentryTunnelAnalyticsModel struct {
	TunnelID types.Int64  `tfsdk:"tunnel_id"`
	JSON     types.String `tfsdk:"json"`
}

func (d *sentryTunnelAnalyticsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel_analytics"
}

func (d *sentryTunnelAnalyticsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads a Sentry tunnel's ingress/egress time series.",
		Attributes: map[string]schema.Attribute{
			"tunnel_id": dsRequiredInt("Sentry tunnel id."),
			"json":      dsStr("`{ingress: [{value, time}], egress: [...], max}` as normalised JSON — use `jsondecode()`."),
		},
	}
}

func (d *sentryTunnelAnalyticsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sentryTunnelAnalyticsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := d.client.Do(ctx, http.MethodGet, fmt.Sprintf("/tunnels/%d/analytics", cfg.TunnelID.ValueInt64()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Sentry tunnel analytics", err.Error())
		return
	}
	cfg.JSON = jsonValue(raw)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewSentryTunnelFiltersDataSource lists the protocol filters a Sentry tunnel
// firewall rule can use.
func NewSentryTunnelFiltersDataSource() datasource.DataSource {
	return &sentryTunnelFiltersDataSource{}
}

type sentryTunnelFiltersDataSource struct{ dataSourceBase }

type sentryFilterModel struct {
	ID           types.Int64  `tfsdk:"id"`
	InternalName types.String `tfsdk:"internal_name"`
	HumanName    types.String `tfsdk:"human_name"`
}

type sentryTunnelFiltersModel struct {
	Filters []sentryFilterModel `tfsdk:"filters"`
}

func (d *sentryTunnelFiltersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel_filters"
}

func (d *sentryTunnelFiltersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the protocol filters available to Sentry tunnel firewall rules (Minecraft Java, FiveM, TCP Generic, …).",
		Attributes: map[string]schema.Attribute{
			"filters": dsList("The filters.", map[string]schema.Attribute{
				"id":            dsInt("Filter id, for `tcpshield_sentry_tunnel_firewall_rule.filter_id`."),
				"internal_name": dsStr("Machine name, e.g. `mc`."),
				"human_name":    dsStr("Display name, e.g. `Minecraft Java`."),
			}),
		},
	}
}

func (d *sentryTunnelFiltersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var list []apiSentryFirewallRule
	if err := d.client.Get(ctx, "/tunnelFilters", &list); err != nil {
		resp.Diagnostics.AddError("Error listing Sentry tunnel filters", err.Error())
		return
	}
	out := sentryTunnelFiltersModel{Filters: make([]sentryFilterModel, len(list))}
	for i, f := range list {
		out.Filters[i] = sentryFilterModel{
			ID:           types.Int64Value(f.ID),
			InternalName: types.StringValue(f.InternalName),
			HumanName:    types.StringValue(f.HumanName),
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// NewSentryTunnelFirewallRulesDataSource lists the firewall rules of a tunnel.
func NewSentryTunnelFirewallRulesDataSource() datasource.DataSource {
	return &sentryTunnelFirewallRulesDataSource{}
}

type sentryTunnelFirewallRulesDataSource struct{ dataSourceBase }

type sentryTunnelFirewallRulesModel struct {
	TunnelID types.Int64               `tfsdk:"tunnel_id"`
	Rules    []sentryFirewallRuleModel `tfsdk:"rules"`
}

func (d *sentryTunnelFirewallRulesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel_firewall_rules"
}

func (d *sentryTunnelFirewallRulesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the firewall rules of a Sentry tunnel.",
		Attributes: map[string]schema.Attribute{
			"tunnel_id": dsRequiredInt("Sentry tunnel id."),
			"rules": dsList("The rules.", map[string]schema.Attribute{
				"tunnel_id":     dsInt("Sentry tunnel id."),
				"id":            dsInt("Numeric rule id."),
				"start_port":    dsInt("First port of the range."),
				"end_port":      dsInt("Last port of the range."),
				"filter_id":     dsInt("Protocol filter id."),
				"is_active":     dsBool("Whether the rule is active."),
				"internal_name": dsStr("Internal name of the rule's filter."),
				"human_name":    dsStr("Display name of the rule's filter."),
				"created_at":    dsStr("Creation timestamp."),
			}),
		},
	}
}

func (d *sentryTunnelFirewallRulesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg sentryTunnelFirewallRulesModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tunnelID := cfg.TunnelID.ValueInt64()
	var list []apiSentryFirewallRule
	if err := d.client.Get(ctx, fmt.Sprintf("/tunnel/%d/tunnelFirewalls", tunnelID), &list); err != nil {
		resp.Diagnostics.AddError("Error listing Sentry tunnel firewall rules", err.Error())
		return
	}
	cfg.Rules = make([]sentryFirewallRuleModel, len(list))
	for i, f := range list {
		cfg.Rules[i].fill(tunnelID, f)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
