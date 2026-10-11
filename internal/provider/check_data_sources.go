package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

// The data sources in this file wrap the API's check-style endpoints: calls
// that answer a question rather than return an entity. Their response shapes
// are undocumented, so each hands the raw answer back as normalised JSON.

// checkCall runs a verification call. A failed verification is an answer, not
// an error — the API reports it as 403 {"error": "Failed to verify domain."} —
// so a 403 comes back as ok=false with the error as the response.
func checkCall(ctx context.Context, d *dataSourceBase, method, path string, b any) (ok bool, response types.String, err error) {
	raw, err := d.client.Do(ctx, method, path, b)
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		out, _ := json.Marshal(map[string]string{"error": apiErr.Message})
		return false, types.StringValue(string(out)), nil
	}
	if err != nil {
		return false, types.StringNull(), err
	}
	return true, jsonValue(raw), nil
}

// NewDomainPreverifyDataSource wraps POST /networks/{id}/domains/preverify.
func NewDomainPreverifyDataSource() datasource.DataSource { return &domainPreverifyDataSource{} }

type domainPreverifyDataSource struct{ dataSourceBase }

type domainPreverifyModel struct {
	NetworkID    types.Int64  `tfsdk:"network_id"`
	DomainName   types.String `tfsdk:"domain_name"`
	Verified     types.Bool   `tfsdk:"verified"`
	ResponseJSON types.String `tfsdk:"response_json"`
}

func (d *domainPreverifyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_preverify"
}

func (d *domainPreverifyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Asks TCPShield whether a domain's DNS already verifies **before** the domain is added to a network. " +
			"Changes nothing; the answer is returned as raw JSON.",
		Attributes: map[string]schema.Attribute{
			"network_id":    dsRequiredInt("Network the domain would be added to."),
			"domain_name":   schema.StringAttribute{MarkdownDescription: "Domain to check.", Required: true},
			"verified":      dsBool("Whether the domain's DNS verifies."),
			"response_json": dsStr("The API's answer, as normalised JSON (`{\"error\": …}` when it does not verify)."),
		},
	}
}

func (d *domainPreverifyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg domainPreverifyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ok, response, err := checkCall(ctx, &d.dataSourceBase, http.MethodPost,
		networkPath(cfg.NetworkID.ValueInt64(), "/domains/preverify"), body{"domain_name": cfg.DomainName.ValueString()})
	if err != nil {
		resp.Diagnostics.AddError("Error pre-verifying domain", err.Error())
		return
	}
	cfg.Verified, cfg.ResponseJSON = types.BoolValue(ok), response
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewDomainVerificationDataSource wraps GET /networks/{id}/domains/{id}/verify.
func NewDomainVerificationDataSource() datasource.DataSource {
	return &domainVerificationDataSource{}
}

type domainVerificationDataSource struct{ dataSourceBase }

type domainVerificationModel struct {
	NetworkID    types.Int64  `tfsdk:"network_id"`
	DomainID     types.Int64  `tfsdk:"domain_id"`
	Verified     types.Bool   `tfsdk:"verified"`
	ResponseJSON types.String `tfsdk:"response_json"`
}

func (d *domainVerificationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_verification"
}

func (d *domainVerificationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Triggers TCPShield's DNS verification check for a domain that is already attached, and reports the outcome. " +
			"Useful after the CNAME/TXT records are in place, instead of waiting for the periodic check.",
		Attributes: map[string]schema.Attribute{
			"network_id":    dsRequiredInt("Network the domain belongs to."),
			"domain_id":     dsRequiredInt("Domain to verify."),
			"verified":      dsBool("The domain's `verified` flag, read back after the check."),
			"response_json": dsStr("The verify call's answer, as normalised JSON (`{\"error\": …}` when it does not verify)."),
		},
	}
}

func (d *domainVerificationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg domainVerificationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID, domainID := cfg.NetworkID.ValueInt64(), cfg.DomainID.ValueInt64()
	_, response, err := checkCall(ctx, &d.dataSourceBase, http.MethodGet,
		networkPath(networkID, fmt.Sprintf("/domains/%d/verify", domainID)), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error verifying domain", err.Error())
		return
	}
	cfg.ResponseJSON = response
	dom, err := readDomain(ctx, d.client, networkID, domainID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading domain after verification", err.Error())
		return
	}
	cfg.Verified = types.BoolValue(dom.Verified)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// analyticsMetrics are the /networks/{id}/analytics/{metric} endpoints.
var analyticsMetrics = []string{"bounceRate", "topDomains", "uniqueUsers", "retention", "mitigatedCount", "mcVersionBreakdown"}

// NewAnalyticsDataSource wraps the network analytics endpoints.
func NewAnalyticsDataSource() datasource.DataSource { return &analyticsDataSource{} }

type analyticsDataSource struct{ dataSourceBase }

type analyticsModel struct {
	NetworkID types.Int64  `tfsdk:"network_id"`
	Metric    types.String `tfsdk:"metric"`
	JSON      types.String `tfsdk:"json"`
}

func (d *analyticsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_analytics"
}

func (d *analyticsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads one analytics metric of a network. Needs advanced analytics (Premium or Enterprise) — " +
			"without it the API answers `403 You do not have analytics access enabled.` The response shapes are not yet verified against the live API.",
		Attributes: map[string]schema.Attribute{
			"network_id": dsRequiredInt("Network to read analytics for."),
			"metric": schema.StringAttribute{
				MarkdownDescription: "One of `bounceRate`, `topDomains`, `uniqueUsers`, `retention`, `mitigatedCount`, `mcVersionBreakdown`.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf(analyticsMetrics...)},
			},
			"json": dsStr("The metric as normalised JSON — use `jsondecode()`; the shape differs per metric."),
		},
	}
}

func (d *analyticsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg analyticsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	raw, err := d.client.Do(ctx, http.MethodGet, networkPath(cfg.NetworkID.ValueInt64(), "/analytics/"+cfg.Metric.ValueString()), nil)
	if err != nil {
		resp.Diagnostics.AddError("Error reading analytics", err.Error())
		return
	}
	cfg.JSON = jsonValue(raw)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
