package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

var (
	_ resource.Resource                = (*sentryTunnelFirewallRuleResource)(nil)
	_ resource.ResourceWithImportState = (*sentryTunnelFirewallRuleResource)(nil)
)

// NewSentryTunnelFirewallRuleResource manages a port range opened on a Sentry
// tunnel, filtered by one of TCPShield's protocol filters.
func NewSentryTunnelFirewallRuleResource() resource.Resource {
	return &sentryTunnelFirewallRuleResource{}
}

type sentryTunnelFirewallRuleResource struct{ resourceBase }

type apiSentryFirewallRule struct {
	ID           int64      `json:"id"`
	StartPort    int64      `json:"start_port"`
	EndPort      int64      `json:"end_port"`
	IsActive     bool       `json:"is_active"`
	InternalName string     `json:"internal_name"`
	HumanName    string     `json:"human_name"`
	FilterID     int64      `json:"filter_id"`
	CreatedAt    flexString `json:"created_at"`
}

type sentryFirewallRuleModel struct {
	TunnelID     types.Int64  `tfsdk:"tunnel_id"`
	ID           types.Int64  `tfsdk:"id"`
	StartPort    types.Int64  `tfsdk:"start_port"`
	EndPort      types.Int64  `tfsdk:"end_port"`
	FilterID     types.Int64  `tfsdk:"filter_id"`
	IsActive     types.Bool   `tfsdk:"is_active"`
	InternalName types.String `tfsdk:"internal_name"`
	HumanName    types.String `tfsdk:"human_name"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (m *sentryFirewallRuleModel) fill(tunnelID int64, f apiSentryFirewallRule) {
	m.TunnelID = types.Int64Value(tunnelID)
	m.ID = types.Int64Value(f.ID)
	m.StartPort = types.Int64Value(f.StartPort)
	m.EndPort = types.Int64Value(f.EndPort)
	m.FilterID = types.Int64Value(f.FilterID)
	m.IsActive = types.BoolValue(f.IsActive)
	m.InternalName = types.StringValue(f.InternalName)
	m.HumanName = types.StringValue(f.HumanName)
	m.CreatedAt = f.CreatedAt.value()
}

func (m *sentryFirewallRuleModel) body() body {
	b := body{
		"start_port": m.StartPort.ValueInt64(),
		"end_port":   m.EndPort.ValueInt64(),
		"filter_id":  m.FilterID.ValueInt64(),
	}
	b.putBool("is_active", m.IsActive, types.BoolNull())
	return b
}

func (r *sentryTunnelFirewallRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sentry_tunnel_firewall_rule"
}

func portAttr(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{
		MarkdownDescription: desc,
		Required:            true,
		Validators:          []validator.Int64{int64validator.Between(1, 65535)},
	}
}

func (r *sentryTunnelFirewallRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A firewall rule on a Sentry tunnel: opens a port range and filters it with one protocol filter " +
			"(see `tcpshield_sentry_tunnel_filters`, e.g. `9` = Minecraft Java, `10` = TCP Generic). **Not yet verified against the live API.**",
		Attributes: map[string]schema.Attribute{
			"tunnel_id":  parentIDAttr("Sentry tunnel the rule belongs to."),
			"id":         idAttr("Numeric rule id."),
			"start_port": portAttr("First port of the range."),
			"end_port":   portAttr("Last port of the range (equal to `start_port` for a single port)."),
			"filter_id": schema.Int64Attribute{
				MarkdownDescription: "Protocol filter id from `tcpshield_sentry_tunnel_filters`.",
				Required:            true,
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
			},
			"is_active":     optBool("Whether the rule is active."),
			"internal_name": computedStr("Internal name of the rule's filter."),
			"human_name":    computedStr("Display name of the rule's filter."),
			"created_at":    computedStr("Creation timestamp."),
		},
	}
}

func sentryFirewallRulePath(id int64) string { return fmt.Sprintf("/tunnelFirewall/%d", id) }

func (r *sentryTunnelFirewallRuleResource) read(ctx context.Context, id int64) (apiSentryFirewallRule, error) {
	var f apiSentryFirewallRule
	raw, err := r.client.Do(ctx, http.MethodGet, sentryFirewallRulePath(id), nil)
	if err == nil {
		err = client.DecodeOne(raw, &f)
	}
	return f, err
}

func (r *sentryTunnelFirewallRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sentryFirewallRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tunnelID := plan.TunnelID.ValueInt64()
	raw, err := r.client.Write(ctx, http.MethodPost, fmt.Sprintf("/tunnel/%d/tunnelFirewall", tunnelID), plan.body(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Sentry tunnel firewall rule", err.Error())
		return
	}
	id, ok := client.CreatedID(raw)
	if !ok {
		id, err = findNewest(ctx, r.client, fmt.Sprintf("/tunnel/%d/tunnelFirewalls", tunnelID), func(f apiSentryFirewallRule) (int64, bool) {
			return f.ID, f.StartPort == plan.StartPort.ValueInt64() && f.EndPort == plan.EndPort.ValueInt64() &&
				f.FilterID == plan.FilterID.ValueInt64()
		})
		if err != nil {
			resp.Diagnostics.AddError("Error locating created Sentry tunnel firewall rule", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tunnel_id"), tunnelID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)

	got, err := r.read(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading created Sentry tunnel firewall rule", err.Error())
		return
	}
	plan.fill(tunnelID, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sentryTunnelFirewallRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sentryFirewallRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := r.read(ctx, state.ID.ValueInt64())
	if client.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading Sentry tunnel firewall rule", err.Error())
		return
	}
	state.fill(state.TunnelID.ValueInt64(), got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *sentryTunnelFirewallRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state sentryFirewallRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueInt64()
	// UpdateSentryTunnelFirewallRequest requires the ports and the filter on
	// every call, so the whole rule is sent.
	if _, err := r.client.Write(ctx, http.MethodPut, sentryFirewallRulePath(id), plan.body(), nil); err != nil {
		resp.Diagnostics.AddError("Error updating Sentry tunnel firewall rule", err.Error())
		return
	}
	got, err := r.read(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading updated Sentry tunnel firewall rule", err.Error())
		return
	}
	plan.fill(state.TunnelID.ValueInt64(), got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *sentryTunnelFirewallRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sentryFirewallRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, sentryFirewallRulePath(state.ID.ValueInt64())); err != nil {
		resp.Diagnostics.AddError("Error deleting Sentry tunnel firewall rule", err.Error())
	}
}

func (r *sentryTunnelFirewallRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportID(req.ID, "tunnel_id", "rule_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tunnel_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[1])...)
}
