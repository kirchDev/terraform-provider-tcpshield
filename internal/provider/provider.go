package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

var _ provider.Provider = (*TCPShieldProvider)(nil)

// TCPShieldProvider is the provider implementation.
type TCPShieldProvider struct {
	// version is set to the release version on build, or "dev" for local builds.
	version string
}

// TCPShieldProviderModel maps provider schema data to a Go type.
type TCPShieldProviderModel struct {
	APIKey   types.String `tfsdk:"api_key"`
	Endpoint types.String `tfsdk:"endpoint"`
}

// New returns a function that instantiates the provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &TCPShieldProvider{version: version}
	}
}

func (p *TCPShieldProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tcpshield"
	resp.Version = p.version
}

func (p *TCPShieldProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [TCPShield](https://tcpshield.com) networks, domains, backend sets, firewalls, Bedrock tunnels and Sentry tunnels as code.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "TCPShield API key (panel → Account → API). May also be set via the `TCPSHIELD_API_KEY` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Base URL of the TCPShield API. Defaults to `https://api.tcpshield.com`. May also be set via `TCPSHIELD_ENDPOINT`.",
				Optional:            true,
			},
		},
	}
}

func (p *TCPShieldProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config TCPShieldProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Unknown TCPShield API key",
			"The provider cannot create the API client because the key is unknown. "+
				"Set it statically or via the TCPSHIELD_API_KEY environment variable.")
		return
	}

	// Env vars are the default; explicit config wins.
	apiKey := os.Getenv("TCPSHIELD_API_KEY")
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}
	endpoint := os.Getenv("TCPSHIELD_ENDPOINT")
	if !config.Endpoint.IsNull() && !config.Endpoint.IsUnknown() {
		endpoint = config.Endpoint.ValueString()
	}

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(path.Root("api_key"), "Missing TCPShield API key",
			"Set the provider `api_key` argument or the TCPSHIELD_API_KEY environment variable.")
		return
	}

	c := client.New(endpoint, apiKey, "terraform-provider-tcpshield/"+p.version)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *TCPShieldProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewASNFirewallRuleResource,
		NewBackendSetResource,
		NewBedrockTunnelResource,
		NewCountryFirewallRuleResource,
		NewDomainResource,
		NewFirewallRuleResource,
		NewNetworkResource,
		NewSentryTunnelFirewallRuleResource,
		NewSentryTunnelResource,
		NewUserProfileResource,
	}
}

func (p *TCPShieldProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAnalyticsDataSource,
		NewASNFirewallRulesDataSource,
		NewBackendSetDataSource,
		NewBackendSetsDataSource,
		NewBedrockTunnelDataSource,
		NewBedrockTunnelLocationsDataSource,
		NewBedrockTunnelsDataSource,
		NewCountryFirewallRulesDataSource,
		NewDomainDataSource,
		NewDomainPreverifyDataSource,
		NewDomainsDataSource,
		NewDomainVerificationDataSource,
		NewFirewallRulesDataSource,
		NewNetworkDataSource,
		NewNetworksDataSource,
		NewSentryTunnelAnalyticsDataSource,
		NewSentryTunnelBindPortDataSource,
		NewSentryTunnelDataSource,
		NewSentryTunnelFiltersDataSource,
		NewSentryTunnelFirewallRulesDataSource,
		NewSentryTunnelLocationsDataSource,
		NewSentryTunnelSetupScriptDataSource,
		NewSentryTunnelsDataSource,
		NewUserDataSource,
	}
}
