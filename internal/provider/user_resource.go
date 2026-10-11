package provider

import (
	"context"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

// apiUser is the "data" object of GET /user/summary.
type apiUser struct {
	ID               int64      `json:"id"`
	Firstname        string     `json:"firstname"`
	Lastname         string     `json:"lastname"`
	Email            string     `json:"email"`
	Phone            string     `json:"phone"`
	AccountLimited   bool       `json:"account_limited"`
	Balance          flexString `json:"balance"`
	PlanID           int64      `json:"plan_id"`
	PlanName         string     `json:"plan_name"`
	PlanExpiry       flexString `json:"plan_expiry"`
	TwoFactorEnabled bool       `json:"two_factor_enabled"`
	Verified         bool       `json:"verified"`
	CommitLevel      flexString `json:"commit_level"`
	CreatedAt        flexString `json:"created_at"`
}

func readUser(ctx context.Context, c *client.Client) (apiUser, error) {
	var env struct {
		Data apiUser `json:"data"`
	}
	err := c.Get(ctx, "/user/summary", &env)
	return env.Data, err
}

// NewUserDataSource reads the account the API key belongs to.
func NewUserDataSource() datasource.DataSource { return &userDataSource{} }

type userDataSource struct{ dataSourceBase }

type userModel struct {
	ID               types.Int64  `tfsdk:"id"`
	Firstname        types.String `tfsdk:"firstname"`
	Lastname         types.String `tfsdk:"lastname"`
	Email            types.String `tfsdk:"email"`
	Phone            types.String `tfsdk:"phone"`
	AccountLimited   types.Bool   `tfsdk:"account_limited"`
	Balance          types.String `tfsdk:"balance"`
	PlanID           types.Int64  `tfsdk:"plan_id"`
	PlanName         types.String `tfsdk:"plan_name"`
	PlanExpiry       types.String `tfsdk:"plan_expiry"`
	TwoFactorEnabled types.Bool   `tfsdk:"two_factor_enabled"`
	Verified         types.Bool   `tfsdk:"verified"`
	CommitLevel      types.String `tfsdk:"commit_level"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (d *userDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (d *userDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "The TCPShield account the API key belongs to — plan, limits and contact details.",
		Attributes: map[string]dschema.Attribute{
			"id":                 dsInt("Numeric user id."),
			"firstname":          dsStr("First name."),
			"lastname":           dsStr("Last name."),
			"email":              dsStr("Login email."),
			"phone":              dsStr("Phone number."),
			"account_limited":    dsBool("Whether the account is limited."),
			"balance":            dsStr("Account balance, as the API reports it."),
			"plan_id":            dsInt("Numeric plan id."),
			"plan_name":          dsStr("Plan name, e.g. `Pro`."),
			"plan_expiry":        dsStr("When the current plan period ends."),
			"two_factor_enabled": dsBool("Whether two-factor authentication is on."),
			"verified":           dsBool("Whether the account's email is verified."),
			"commit_level":       dsStr("Commit level, empty when none."),
			"created_at":         dsStr("Account creation timestamp."),
		},
	}
}

func (d *userDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	u, err := readUser(ctx, d.client)
	if err != nil {
		resp.Diagnostics.AddError("Error reading user summary", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &userModel{
		ID:               types.Int64Value(u.ID),
		Firstname:        types.StringValue(u.Firstname),
		Lastname:         types.StringValue(u.Lastname),
		Email:            types.StringValue(u.Email),
		Phone:            types.StringValue(u.Phone),
		AccountLimited:   types.BoolValue(u.AccountLimited),
		Balance:          u.Balance.value(),
		PlanID:           types.Int64Value(u.PlanID),
		PlanName:         types.StringValue(u.PlanName),
		PlanExpiry:       u.PlanExpiry.value(),
		TwoFactorEnabled: types.BoolValue(u.TwoFactorEnabled),
		Verified:         types.BoolValue(u.Verified),
		CommitLevel:      u.CommitLevel.value(),
		CreatedAt:        u.CreatedAt.value(),
	})...)
}

// --- tcpshield_user_profile ---

var (
	_ resource.Resource                = (*userProfileResource)(nil)
	_ resource.ResourceWithImportState = (*userProfileResource)(nil)
)

// NewUserProfileResource manages the account's general info. It is a singleton:
// create adopts the account, destroy only forgets it.
func NewUserProfileResource() resource.Resource { return &userProfileResource{} }

type userProfileResource struct{ resourceBase }

type userProfileModel struct {
	ID        types.Int64  `tfsdk:"id"`
	Firstname types.String `tfsdk:"firstname"`
	Lastname  types.String `tfsdk:"lastname"`
	Phone     types.String `tfsdk:"phone"`
}

func (m *userProfileModel) fill(u apiUser) {
	m.ID = types.Int64Value(u.ID)
	m.Firstname = types.StringValue(u.Firstname)
	m.Lastname = types.StringValue(u.Lastname)
	m.Phone = types.StringValue(u.Phone)
}

func (r *userProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_profile"
}

func (r *userProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The general info (name, phone) of the account the API key belongs to. A singleton: creating it adopts " +
			"the account, destroying it only removes it from state. The API takes all three fields on every update and rejects an " +
			"empty one (`PhoneNumber is a required field`) — so all three are required, and a phone number, once set, cannot be " +
			"removed again through the API. Email, password and API key are deliberately not managed — see the provider README.",
		Attributes: map[string]schema.Attribute{
			"id":        idAttr("Numeric user id."),
			"firstname": profileAttr("First name."),
			"lastname":  profileAttr("Last name."),
			"phone":     profileAttr("Phone number."),
		},
	}
}

func profileAttr(desc string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: desc,
		Required:            true,
		Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
	}
}

func (r *userProfileResource) apply(ctx context.Context, plan, prior *userProfileModel) (apiUser, error) {
	if !plan.Firstname.Equal(prior.Firstname) || !plan.Lastname.Equal(prior.Lastname) || !plan.Phone.Equal(prior.Phone) {
		// The endpoint validates all three fields on every call.
		b := body{
			"firstname": plan.Firstname.ValueString(),
			"lastname":  plan.Lastname.ValueString(),
			"phone":     plan.Phone.ValueString(),
		}
		if _, err := r.client.Write(ctx, http.MethodPost, "/user/updateGeneralInfo", b, nil); err != nil {
			return apiUser{}, err
		}
	}
	return readUser(ctx, r.client)
}

func (r *userProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan userProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current, err := readUser(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Error reading user summary", err.Error())
		return
	}
	var prior userProfileModel
	prior.fill(current)
	u, err := r.apply(ctx, &plan, &prior)
	if err != nil {
		resp.Diagnostics.AddError("Error updating user profile", err.Error())
		return
	}
	plan.fill(u)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state userProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	u, err := readUser(ctx, r.client)
	if err != nil {
		resp.Diagnostics.AddError("Error reading user summary", err.Error())
		return
	}
	state.fill(u)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *userProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	u, err := r.apply(ctx, &plan, &state)
	if err != nil {
		resp.Diagnostics.AddError("Error updating user profile", err.Error())
		return
	}
	plan.fill(u)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete only forgets the profile: the account outlives the configuration.
func (r *userProfileResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}

// ImportState adopts the account. There is only one, so the import id is not
// looked at — Read fills in the real user id.
func (r *userProfileResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), int64(0))...)
}
