package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

var (
	_ resource.Resource                = (*networkResource)(nil)
	_ resource.ResourceWithImportState = (*networkResource)(nil)
)

// NewNetworkResource manages a TCPShield network.
func NewNetworkResource() resource.Resource { return &networkResource{} }

type networkResource struct{ resourceBase }

// apiNetwork is the wire shape of GET /networks/{id}.
type apiNetwork struct {
	ID                            int64      `json:"id"`
	Name                          string     `json:"name"`
	Premium                       bool       `json:"premium"`
	ProtectedCname                string     `json:"protected_cname"`
	TxtVerification               string     `json:"txt_verification"`
	ConnectionsPerSecondThreshold int64      `json:"connections_per_second_threshold"`
	ClientBanSeconds              int64      `json:"client_ban_seconds"`
	ClientAllowSeconds            int64      `json:"client_allow_seconds"`
	MitigationMessage             string     `json:"mitigation_message"`
	VpnBlock                      bool       `json:"vpn_block"`
	WebhookURL                    string     `json:"webhook_url"`
	AttackEmails                  bool       `json:"attack_emails"`
	VerificationServer            bool       `json:"verification_server"`
	AntivpnKickMsg                string     `json:"antivpn_kick_msg"`
	ForwardTrueSlp                bool       `json:"forward_true_slp"`
	OfflineMotdEnabled            bool       `json:"offline_motd_enabled"`
	OfflineMotd                   string     `json:"offline_motd"`
	OfflineKickMessage            string     `json:"offline_kick_message"`
	OfflineFavicon                string     `json:"offline_favicon"`
	CreatedAt                     flexString `json:"created_at"`
	UpdatedAt                     flexString `json:"updated_at"`
}

type networkModel struct {
	ID                            types.Int64  `tfsdk:"id"`
	Name                          types.String `tfsdk:"name"`
	Premium                       types.Bool   `tfsdk:"premium"`
	ProtectedCname                types.String `tfsdk:"protected_cname"`
	TxtVerification               types.String `tfsdk:"txt_verification"`
	ConnectionsPerSecondThreshold types.Int64  `tfsdk:"connections_per_second_threshold"`
	ClientBanSeconds              types.Int64  `tfsdk:"client_ban_seconds"`
	ClientAllowSeconds            types.Int64  `tfsdk:"client_allow_seconds"`
	MitigationMessage             types.String `tfsdk:"mitigation_message"`
	VpnBlock                      types.Bool   `tfsdk:"vpn_block"`
	WebhookURL                    types.String `tfsdk:"webhook_url"`
	AttackEmails                  types.Bool   `tfsdk:"attack_emails"`
	VerificationServer            types.Bool   `tfsdk:"verification_server"`
	AntivpnKickMsg                types.String `tfsdk:"antivpn_kick_msg"`
	ForwardTrueSlp                types.Bool   `tfsdk:"forward_true_slp"`
	OfflineMotdEnabled            types.Bool   `tfsdk:"offline_motd_enabled"`
	OfflineMotd                   types.String `tfsdk:"offline_motd"`
	OfflineKickMessage            types.String `tfsdk:"offline_kick_message"`
	OfflineFavicon                types.String `tfsdk:"offline_favicon"`
	CreatedAt                     types.String `tfsdk:"created_at"`
}

func (m *networkModel) fill(n apiNetwork) {
	m.ID = types.Int64Value(n.ID)
	m.Name = types.StringValue(n.Name)
	m.Premium = types.BoolValue(n.Premium)
	m.ProtectedCname = types.StringValue(n.ProtectedCname)
	m.TxtVerification = types.StringValue(n.TxtVerification)
	m.ConnectionsPerSecondThreshold = types.Int64Value(n.ConnectionsPerSecondThreshold)
	m.ClientBanSeconds = types.Int64Value(n.ClientBanSeconds)
	m.ClientAllowSeconds = types.Int64Value(n.ClientAllowSeconds)
	m.MitigationMessage = types.StringValue(n.MitigationMessage)
	m.VpnBlock = types.BoolValue(n.VpnBlock)
	m.WebhookURL = types.StringValue(n.WebhookURL)
	m.AttackEmails = types.BoolValue(n.AttackEmails)
	m.VerificationServer = types.BoolValue(n.VerificationServer)
	m.AntivpnKickMsg = types.StringValue(n.AntivpnKickMsg)
	m.ForwardTrueSlp = types.BoolValue(n.ForwardTrueSlp)
	m.OfflineMotdEnabled = types.BoolValue(n.OfflineMotdEnabled)
	m.OfflineMotd = types.StringValue(n.OfflineMotd)
	m.OfflineKickMessage = types.StringValue(n.OfflineKickMessage)
	m.OfflineFavicon = types.StringValue(n.OfflineFavicon)
	m.CreatedAt = n.CreatedAt.value()
}

// settings is the PATCH body: every setting that differs from prior.
func (m *networkModel) settings(prior *networkModel) body {
	b := body{}
	b.putString("name", m.Name, prior.Name)
	b.putInt64("connections_per_second_threshold", m.ConnectionsPerSecondThreshold, prior.ConnectionsPerSecondThreshold)
	b.putInt64("client_ban_seconds", m.ClientBanSeconds, prior.ClientBanSeconds)
	b.putInt64("client_allow_seconds", m.ClientAllowSeconds, prior.ClientAllowSeconds)
	b.putString("mitigation_message", m.MitigationMessage, prior.MitigationMessage)
	b.putBool("vpn_block", m.VpnBlock, prior.VpnBlock)
	b.putString("webhook_url", m.WebhookURL, prior.WebhookURL)
	b.putBool("attack_emails", m.AttackEmails, prior.AttackEmails)
	b.putBool("verification_server", m.VerificationServer, prior.VerificationServer)
	b.putString("antivpn_kick_msg", m.AntivpnKickMsg, prior.AntivpnKickMsg)
	b.putBool("forward_true_slp", m.ForwardTrueSlp, prior.ForwardTrueSlp)
	b.putBool("offline_motd_enabled", m.OfflineMotdEnabled, prior.OfflineMotdEnabled)
	b.putString("offline_motd", m.OfflineMotd, prior.OfflineMotd)
	b.putString("offline_kick_message", m.OfflineKickMessage, prior.OfflineKickMessage)
	b.putString("offline_favicon", m.OfflineFavicon, prior.OfflineFavicon)
	return b
}

func (r *networkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func optStr(desc string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc,
		Optional:            true,
		Computed:            true,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func optInt(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{
		MarkdownDescription: desc,
		Optional:            true,
		Computed:            true,
		PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
	}
}

func optBool(desc string) schema.BoolAttribute {
	return schema.BoolAttribute{
		MarkdownDescription: desc,
		Optional:            true,
		Computed:            true,
		PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
	}
}

// The network PATCH silently ignores zero values for these fields (0, ""), so
// sending one would leave the old value live and fail the apply with an
// inconsistent result. The validators turn that into a plan-time error.
func withIntRange(a schema.Int64Attribute, lo, hi int64) schema.Int64Attribute {
	a.Validators = append(a.Validators, int64validator.Between(lo, hi))
	return a
}

func withNonEmpty(a schema.StringAttribute) schema.StringAttribute {
	a.Validators = append(a.Validators, stringvalidator.LengthAtLeast(1))
	return a
}

func computedStr(desc string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc,
		Computed:            true,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func computedBool(desc string) schema.BoolAttribute {
	return schema.BoolAttribute{
		MarkdownDescription: desc,
		Computed:            true,
		PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
	}
}

func idAttr(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{
		MarkdownDescription: desc,
		Computed:            true,
		PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
	}
}

func parentIDAttr(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{
		MarkdownDescription: desc,
		Required:            true,
		PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
	}
}

func (r *networkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A TCPShield network — the unit that owns domains, backend sets, firewalls and Bedrock tunnels. " +
			"Every setting is Optional + Computed: one left out of the configuration keeps the value TCPShield holds. " +
			"Destroying it deletes the network and everything attached to it.",
		Attributes: map[string]schema.Attribute{
			"id": idAttr("Numeric network id."),
			"name": schema.StringAttribute{
				MarkdownDescription: "Network name.",
				Required:            true,
			},
			"premium":                          computedBool("Whether the network runs on a premium plan."),
			"protected_cname":                  computedStr("CNAME the network's domains point at to be protected."),
			"txt_verification":                 computedStr("TXT record value that proves ownership of the network's domains."),
			"connections_per_second_threshold": withIntRange(optInt("Connections per second above which mitigation starts, 1–100."), 1, 100),
			"client_ban_seconds":               withIntRange(optInt("Seconds a client stays banned once mitigation flags it, 1–3600."), 1, 3600),
			"client_allow_seconds":             withIntRange(optInt("Seconds a client stays allowed after it passed verification, 1–600."), 1, 600),
			"mitigation_message":               withNonEmpty(optStr("Kick message shown to a client during mitigation. Cannot be emptied — the API ignores `\"\"`.")),
			"vpn_block":                        optBool("Block connections from known VPNs and proxies."),
			"webhook_url":                      optStr("Discord or Slack webhook notified when an attack starts or ends — the API rejects any other URL."),
			"attack_emails":                    optBool("Send an email when an attack is detected."),
			"verification_server":              optBool("Route players through the verification server. Enterprise only: on any other plan the API answers `Your plan does not include the verification server.`"),
			"antivpn_kick_msg":                 withNonEmpty(optStr("Kick message for a client blocked by `vpn_block`. Cannot be emptied — the API ignores `\"\"`.")),
			"forward_true_slp":                 optBool("Forward server list pings to the backend instead of answering them at the edge."),
			"offline_motd_enabled":             optBool("Answer pings with `offline_motd` while every backend is down."),
			"offline_motd":                     optStr("MOTD shown while every backend is down."),
			"offline_kick_message":             optStr("Kick message shown while every backend is down."),
			"offline_favicon":                  optStr("Base64 favicon shown while every backend is down."),
			"created_at":                       computedStr("Creation timestamp."),
		},
	}
}

func (r *networkResource) read(ctx context.Context, id int64) (apiNetwork, error) {
	var n apiNetwork
	err := r.client.Get(ctx, networkPath(id, ""), &n)
	return n, err
}

func (r *networkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan networkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	raw, err := r.client.Write(ctx, http.MethodPost, "/networks", body{"name": plan.Name.ValueString()}, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating network", err.Error())
		return
	}
	id, ok := client.CreatedID(raw)
	if !ok {
		id, err = findNewest(ctx, r.client, "/networks", func(n apiNetwork) (int64, bool) {
			return n.ID, n.Name == plan.Name.ValueString()
		})
		if err != nil {
			resp.Diagnostics.AddError("Error locating created network", err.Error())
			return
		}
	}
	// Record the id first, so a failure in the follow-up update leaves a
	// tainted resource in state rather than an orphaned network.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)

	created, err := r.read(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading created network", err.Error())
		return
	}
	var prior networkModel
	prior.fill(created)

	if patch := plan.settings(&prior); len(patch) > 0 {
		if _, err := r.client.Write(ctx, http.MethodPatch, networkPath(id, ""), patch, nil); err != nil {
			resp.Diagnostics.AddError("Error applying network settings", err.Error())
			return
		}
		if created, err = r.read(ctx, id); err != nil {
			resp.Diagnostics.AddError("Error reading created network", err.Error())
			return
		}
	}
	plan.fill(created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	n, err := r.read(ctx, state.ID.ValueInt64())
	if client.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading network", err.Error())
		return
	}
	state.fill(n)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *networkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state networkModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueInt64()
	if patch := plan.settings(&state); len(patch) > 0 {
		if _, err := r.client.Write(ctx, http.MethodPatch, networkPath(id, ""), patch, nil); err != nil {
			resp.Diagnostics.AddError("Error updating network", err.Error())
			return
		}
	}
	n, err := r.read(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading updated network", err.Error())
		return
	}
	plan.fill(n)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state networkModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, networkPath(state.ID.ValueInt64(), "")); err != nil {
		resp.Diagnostics.AddError("Error deleting network", err.Error())
	}
}

func (r *networkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportID(req.ID, "network_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[0])...)
}
