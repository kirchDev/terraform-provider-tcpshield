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
	_ resource.Resource                = (*domainResource)(nil)
	_ resource.ResourceWithImportState = (*domainResource)(nil)
)

// NewDomainResource manages a domain attached to a network.
func NewDomainResource() resource.Resource { return &domainResource{} }

type domainResource struct{ resourceBase }

type apiDomain struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	BackendSetID int64      `json:"backend_set_id"`
	BAC          bool       `json:"bac"`
	Verified     bool       `json:"verified"`
	CreatedAt    flexString `json:"created_at"`
}

type domainModel struct {
	NetworkID    types.Int64  `tfsdk:"network_id"`
	ID           types.Int64  `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	BackendSetID types.Int64  `tfsdk:"backend_set_id"`
	BAC          types.Bool   `tfsdk:"bac"`
	Verified     types.Bool   `tfsdk:"verified"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

func (m *domainModel) fill(networkID int64, d apiDomain) {
	m.NetworkID = types.Int64Value(networkID)
	m.ID = types.Int64Value(d.ID)
	m.Name = types.StringValue(d.Name)
	m.BackendSetID = types.Int64Value(d.BackendSetID)
	m.BAC = types.BoolValue(d.BAC)
	m.Verified = types.BoolValue(d.Verified)
	m.CreatedAt = d.CreatedAt.value()
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A domain attached to a network and routed to a backend set. TCPShield only serves it once " +
			"`verified` is true — point a CNAME at the network's `protected_cname` and add its `txt_verification` record.",
		Attributes: map[string]schema.Attribute{
			"network_id": parentIDAttr("Network the domain belongs to."),
			"id":         idAttr("Numeric domain id."),
			"name": schema.StringAttribute{
				MarkdownDescription: "Fully qualified domain name, e.g. `play.example.com`.",
				Required:            true,
			},
			"backend_set_id": schema.Int64Attribute{
				MarkdownDescription: "Backend set the domain forwards to.",
				Required:            true,
			},
			"bac":        optBool("The domain's BAC switch, as shown in the TCPShield panel."),
			"verified":   computedBool("Whether TCPShield has verified the domain's DNS."),
			"created_at": computedStr("Creation timestamp."),
		},
	}
}

func domainPath(networkID, id int64) string {
	return networkPath(networkID, fmt.Sprintf("/domains/%d", id))
}

func readDomain(ctx context.Context, c *client.Client, networkID, id int64) (apiDomain, error) {
	var d apiDomain
	raw, err := c.Do(ctx, http.MethodGet, domainPath(networkID, id), nil)
	if err == nil {
		err = client.DecodeOne(raw, &d)
	}
	return d, err
}

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID := plan.NetworkID.ValueInt64()
	b := body{"name": plan.Name.ValueString(), "backend_set_id": plan.BackendSetID.ValueInt64()}
	b.putBool("bac", plan.BAC, types.BoolNull())
	raw, err := r.client.Write(ctx, http.MethodPost, networkPath(networkID, "/domains"), b, nil)
	if err != nil {
		resp.Diagnostics.AddError("Error creating domain", err.Error())
		return
	}
	id, ok := client.CreatedID(raw)
	if !ok {
		id, err = findNewest(ctx, r.client, networkPath(networkID, "/domains"), func(d apiDomain) (int64, bool) {
			return d.ID, d.Name == plan.Name.ValueString()
		})
		if err != nil {
			resp.Diagnostics.AddError("Error locating created domain", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), networkID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)

	got, err := readDomain(ctx, r.client, networkID, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading created domain", err.Error())
		return
	}
	plan.fill(networkID, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	got, err := readDomain(ctx, r.client, state.NetworkID.ValueInt64(), state.ID.ValueInt64())
	if client.NotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Error reading domain", err.Error())
		return
	}
	state.fill(state.NetworkID.ValueInt64(), got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID, id := state.NetworkID.ValueInt64(), state.ID.ValueInt64()
	// UpdateDomainRequest requires name, backend_set_id and bac on every call
	// ("Name is a required field"), so the whole domain is sent.
	b := body{
		"name":           plan.Name.ValueString(),
		"backend_set_id": plan.BackendSetID.ValueInt64(),
		"bac":            plan.BAC.ValueBool(),
	}
	if _, err := r.client.Write(ctx, http.MethodPatch, domainPath(networkID, id), b, nil); err != nil {
		resp.Diagnostics.AddError("Error updating domain", err.Error())
		return
	}
	got, err := readDomain(ctx, r.client, networkID, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading updated domain", err.Error())
		return
	}
	plan.fill(networkID, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Delete(ctx, domainPath(state.NetworkID.ValueInt64(), state.ID.ValueInt64())); err != nil {
		resp.Diagnostics.AddError("Error deleting domain", err.Error())
	}
}

func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	ids, err := parseImportID(req.ID, "network_id", "domain_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid import id", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("network_id"), ids[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ids[1])...)
}

// --- data sources ---

func domainDSAttrs() map[string]dschema.Attribute {
	return map[string]dschema.Attribute{
		"network_id":     dsInt("Network the domain belongs to."),
		"id":             dsInt("Numeric domain id."),
		"name":           dsStr("Fully qualified domain name."),
		"backend_set_id": dsInt("Backend set the domain forwards to."),
		"bac":            dsBool("The domain's BAC switch, as shown in the TCPShield panel."),
		"verified":       dsBool("Whether TCPShield has verified the domain's DNS."),
		"created_at":     dsStr("Creation timestamp."),
	}
}

// NewDomainDataSource reads one domain.
func NewDomainDataSource() datasource.DataSource { return &domainDataSource{} }

type domainDataSource struct{ dataSourceBase }

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Reads a single domain of a network, by `id` or by `name` (exactly one of them).",
		Attributes: with(domainDSAttrs(), map[string]dschema.Attribute{
			"network_id": dsRequiredInt("Network the domain belongs to."),
			"id":         dschema.Int64Attribute{MarkdownDescription: "Numeric domain id.", Optional: true, Computed: true},
			"name":       dschema.StringAttribute{MarkdownDescription: "Fully qualified domain name.", Optional: true, Computed: true},
		}),
	}
}

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg domainModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	networkID := cfg.NetworkID.ValueInt64()
	var got apiDomain
	switch {
	case !cfg.ID.IsNull():
		var err error
		if got, err = readDomain(ctx, d.client, networkID, cfg.ID.ValueInt64()); err != nil {
			resp.Diagnostics.AddError("Error reading domain", err.Error())
			return
		}
	case !cfg.Name.IsNull():
		var list []apiDomain
		if err := d.client.Get(ctx, networkPath(networkID, "/domains"), &list); err != nil {
			resp.Diagnostics.AddError("Error listing domains", err.Error())
			return
		}
		found := false
		for _, x := range list {
			if x.Name == cfg.Name.ValueString() {
				got, found = x, true
				break
			}
		}
		if !found {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Domain not found",
				fmt.Sprintf("network %d has no domain named %q", networkID, cfg.Name.ValueString()))
			return
		}
	default:
		resp.Diagnostics.AddError("Missing lookup key", "Set either `id` or `name`.")
		return
	}
	cfg.fill(networkID, got)
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// NewDomainsDataSource lists a network's domains.
func NewDomainsDataSource() datasource.DataSource { return &domainsDataSource{} }

type domainsDataSource struct{ dataSourceBase }

type domainsModel struct {
	NetworkID types.Int64   `tfsdk:"network_id"`
	Domains   []domainModel `tfsdk:"domains"`
}

func (d *domainsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domains"
}

func (d *domainsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Lists the domains attached to a network.",
		Attributes: map[string]dschema.Attribute{
			"network_id": dsRequiredInt("Network to list."),
			"domains":    dsList("The domains.", domainDSAttrs()),
		},
	}
}

func (d *domainsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg domainsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []apiDomain
	if err := d.client.Get(ctx, networkPath(cfg.NetworkID.ValueInt64(), "/domains"), &list); err != nil {
		resp.Diagnostics.AddError("Error listing domains", err.Error())
		return
	}
	cfg.Domains = make([]domainModel, len(list))
	for i, x := range list {
		cfg.Domains[i].fill(cfg.NetworkID.ValueInt64(), x)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
