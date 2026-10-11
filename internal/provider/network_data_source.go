package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var stringType = types.StringType

func networkDSAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":                               dsInt("Numeric network id."),
		"name":                             dsStr("Network name."),
		"premium":                          dsBool("Whether the network runs on a premium plan."),
		"protected_cname":                  dsStr("CNAME the network's domains point at to be protected."),
		"txt_verification":                 dsStr("TXT record value that proves ownership of the network's domains."),
		"connections_per_second_threshold": dsInt("Connections per second above which mitigation starts."),
		"client_ban_seconds":               dsInt("Seconds a client stays banned once mitigation flags it."),
		"client_allow_seconds":             dsInt("Seconds a client stays allowed after it passed verification."),
		"mitigation_message":               dsStr("Kick message shown to a client during mitigation."),
		"vpn_block":                        dsBool("Whether connections from known VPNs and proxies are blocked."),
		"webhook_url":                      dsStr("URL notified when an attack starts or ends."),
		"attack_emails":                    dsBool("Whether an email is sent when an attack is detected."),
		"verification_server":              dsBool("Whether players are routed through the verification server."),
		"antivpn_kick_msg":                 dsStr("Kick message for a client blocked by `vpn_block`."),
		"forward_true_slp":                 dsBool("Whether server list pings are forwarded to the backend."),
		"offline_motd_enabled":             dsBool("Whether `offline_motd` answers pings while every backend is down."),
		"offline_motd":                     dsStr("MOTD shown while every backend is down."),
		"offline_kick_message":             dsStr("Kick message shown while every backend is down."),
		"offline_favicon":                  dsStr("Base64 favicon shown while every backend is down."),
		"created_at":                       dsStr("Creation timestamp."),
	}
}

// NewNetworkDataSource looks up one network by id or by name.
func NewNetworkDataSource() datasource.DataSource { return &networkDataSource{} }

type networkDataSource struct{ dataSourceBase }

func (d *networkDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (d *networkDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single TCPShield network by `id` or by `name` (exactly one of them).",
		Attributes: with(networkDSAttrs(), map[string]schema.Attribute{
			"id":   schema.Int64Attribute{MarkdownDescription: "Numeric network id.", Optional: true, Computed: true},
			"name": schema.StringAttribute{MarkdownDescription: "Network name. Must match exactly one network.", Optional: true, Computed: true},
		}),
	}
}

func (d *networkDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg networkModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var n apiNetwork
	switch {
	case !cfg.ID.IsNull():
		if err := d.client.Get(ctx, networkPath(cfg.ID.ValueInt64(), ""), &n); err != nil {
			resp.Diagnostics.AddError("Error reading network", err.Error())
			return
		}
	case !cfg.Name.IsNull():
		var list []apiNetwork
		if err := d.client.Get(ctx, "/networks", &list); err != nil {
			resp.Diagnostics.AddError("Error listing networks", err.Error())
			return
		}
		var matches []apiNetwork
		for _, x := range list {
			if x.Name == cfg.Name.ValueString() {
				matches = append(matches, x)
			}
		}
		if len(matches) != 1 {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Network lookup failed",
				fmt.Sprintf("expected exactly one network named %q, found %d", cfg.Name.ValueString(), len(matches)))
			return
		}
		n = matches[0]
	default:
		resp.Diagnostics.AddError("Missing lookup key", "Set either `id` or `name`.")
		return
	}

	cfg.fill(n)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewNetworksDataSource lists every network on the account.
func NewNetworksDataSource() datasource.DataSource { return &networksDataSource{} }

type networksDataSource struct{ dataSourceBase }

type networksModel struct {
	Networks []networkModel `tfsdk:"networks"`
}

func (d *networksDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_networks"
}

func (d *networksDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every TCPShield network the API key can see.",
		Attributes: map[string]schema.Attribute{
			"networks": dsList("The networks.", networkDSAttrs()),
		},
	}
}

func (d *networksDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var list []apiNetwork
	if err := d.client.Get(ctx, "/networks", &list); err != nil {
		resp.Diagnostics.AddError("Error listing networks", err.Error())
		return
	}
	out := networksModel{Networks: make([]networkModel, len(list))}
	for i, n := range list {
		out.Networks[i].fill(n)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}
