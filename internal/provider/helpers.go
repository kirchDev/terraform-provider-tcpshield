package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

// resourceBase gives a resource the configured API client.
type resourceBase struct {
	client *client.Client
}

func (b *resourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	b.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

// dataSourceBase gives a data source the configured API client.
type dataSourceBase struct {
	client *client.Client
}

func (b *dataSourceBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	b.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func clientFrom(data any, diags *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil // provider not configured yet (validate/plan before Configure)
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T", data))
		return nil
	}
	return c
}

// parseImportID splits an import id of the form "a/b/…" into exactly n positive
// integers. The parts name the parents first and the entity last, matching the
// URL path the entity lives under.
func parseImportID(id string, names ...string) ([]int64, error) {
	parts := strings.Split(id, "/")
	if len(parts) != len(names) {
		return nil, fmt.Errorf("expected import id %q, got %q", strings.Join(names, "/"), id)
	}
	out := make([]int64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("import id part %q (%s) is not a positive integer", p, names[i])
		}
		out[i] = n
	}
	return out, nil
}

// body is a JSON request body built from the plan. Its put* methods add a key
// only when the planned value is known and differs from prior — on create prior
// is null, so every configured value is sent; on update only what moved is. An
// echo of an unchanged value is a write the practitioner never asked for.
type body map[string]any

func (b body) putString(key string, plan, prior types.String) {
	if plan.IsNull() || plan.IsUnknown() || plan.Equal(prior) {
		return
	}
	b[key] = plan.ValueString()
}

func (b body) putInt64(key string, plan, prior types.Int64) {
	if plan.IsNull() || plan.IsUnknown() || plan.Equal(prior) {
		return
	}
	b[key] = plan.ValueInt64()
}

func (b body) putBool(key string, plan, prior types.Bool) {
	if plan.IsNull() || plan.IsUnknown() || plan.Equal(prior) {
		return
	}
	b[key] = plan.ValueBool()
}

func (b body) putStringList(ctx context.Context, key string, plan, prior types.List, diags *diag.Diagnostics) {
	if plan.IsNull() || plan.IsUnknown() || plan.Equal(prior) {
		return
	}
	var v []string
	diags.Append(plan.ElementsAs(ctx, &v, false)...)
	if v == nil {
		v = []string{}
	}
	b[key] = v
}

// stringList converts a Go slice into a framework list.
func stringList(v []string) types.List {
	elems := make([]attr.Value, len(v))
	for i, s := range v {
		elems[i] = types.StringValue(s)
	}
	return types.ListValueMust(types.StringType, elems)
}

// flexString decodes any JSON scalar as a string. The API is inconsistent about
// types — /user/summary sends balance as "0" although the spec says float — and
// a decode error on a field nobody configures should not fail a whole read.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*f = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(b, &str); err == nil {
		*f = flexString(str)
		return nil
	}
	*f = flexString(s)
	return nil
}

func (f flexString) value() types.String { return types.StringValue(string(f)) }

// diagsT shortens the diagnostics pointer type in model helpers.
type diagsT = diag.Diagnostics

// findNewest resolves the id of an entity the create call did not return, by
// listing the collection and picking the highest id that matches. Several
// entities may share the matched values; the newest is the one just created.
func findNewest[T any](ctx context.Context, c *client.Client, collection string, match func(T) (int64, bool)) (int64, error) {
	var list []T
	if err := c.Get(ctx, collection, &list); err != nil {
		return 0, err
	}
	var best int64
	for _, x := range list {
		if id, ok := match(x); ok && id > best {
			best = id
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("the entity was created but does not appear in GET %s", collection)
	}
	return best, nil
}

func networkPath(networkID int64, rest string) string {
	return fmt.Sprintf("/networks/%d%s", networkID, rest)
}

// jsonValue renders raw API JSON as a normalised (compact) string for the
// *_json attributes, so whitespace in the response never surfaces as a diff.
func jsonValue(raw json.RawMessage) types.String {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return types.StringValue(strings.TrimSpace(string(raw)))
	}
	b, _ := json.Marshal(v)
	return types.StringValue(string(b))
}
