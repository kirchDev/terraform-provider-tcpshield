package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMockNetworkLifecycle drives the live test's configuration through the
// mock, so CI covers the same create → update → import → destroy cycle without
// an account.
func TestAccMockNetworkLifecycle(t *testing.T) {
	newMockTCPShield(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: liveConfig("tf-acc-mock", "tf-acc-mock.invalid", liveV1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_network.n", "client_ban_seconds", "120"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "backends.#", "2"),
					resource.TestCheckResourceAttr("tcpshield_country_firewall_rule.c", "country_code", "AD"),
					resource.TestCheckResourceAttr("data.tcpshield_domain_verification.v", "verified", "false"),
					resource.TestCheckResourceAttr("data.tcpshield_backend_sets.all", "backend_sets.0.backends.#", "2"),
				),
			},
			{
				Config: liveConfig("tf-acc-mock", "tf-acc-mock.invalid", liveV2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "load_balancing_mode", "1"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "backends.#", "1"),
					resource.TestCheckResourceAttr("tcpshield_domain.d", "bac", "true"),
					resource.TestCheckResourceAttrPair("tcpshield_domain.d", "backend_set_id", "tcpshield_backend_set.b", "id"),
					resource.TestCheckResourceAttr("tcpshield_firewall_rule.ip", "whitelist", "true"),
				),
			},
			liveImport("tcpshield_network.n", nil),
			liveImport("tcpshield_backend_set.a", []string{"network_id"}),
			liveImport("tcpshield_domain.d", []string{"network_id"}),
			liveImport("tcpshield_firewall_rule.ip", []string{"network_id"}),
			liveImport("tcpshield_country_firewall_rule.c", []string{"network_id"}),
		},
	})
}

// TestAccMockPartialBackendSetUpdate pins the PATCH-is-a-PUT handling: a change
// to one flag must not reset load_balancing_mode or drop the backends.
func TestAccMockPartialBackendSetUpdate(t *testing.T) {
	newMockTCPShield(t)
	cfg := func(vulcan bool) string {
		return fmt.Sprintf(`
resource "tcpshield_network" "n" { name = "n" }
resource "tcpshield_backend_set" "a" {
  network_id          = tcpshield_network.n.id
  name                = "a"
  backends            = ["192.0.2.1:25565"]
  load_balancing_mode = 1
  vulcan_ac_enabled   = %t
}`, vulcan)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: cfg(false)},
			{
				Config: cfg(true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "vulcan_ac_enabled", "true"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "load_balancing_mode", "1"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "backends.0", "192.0.2.1:25565"),
				),
			},
		},
	})
}

func TestAccMockBedrockTunnel(t *testing.T) {
	newMockTCPShield(t)
	cfg := func(location string, pp int) string {
		return fmt.Sprintf(`
resource "tcpshield_network" "n" { name = "n" }
resource "tcpshield_backend_set" "a" {
  network_id = tcpshield_network.n.id
  name       = "a"
  backends   = ["192.0.2.1:19132"]
}
resource "tcpshield_bedrock_tunnel" "t" {
  network_id     = tcpshield_network.n.id
  name           = "bedrock"
  backend_set_id = tcpshield_backend_set.a.id
  location       = %q
  proxy_protocol = %d
}
data "tcpshield_bedrock_tunnels" "all" {
  network_id = tcpshield_network.n.id
  depends_on = [tcpshield_bedrock_tunnel.t]
}
data "tcpshield_bedrock_tunnel_locations" "l" { network_id = tcpshield_network.n.id }`, location, pp)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg("FRA", 0),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("tcpshield_bedrock_tunnel.t", "allocated_cname"),
					resource.TestCheckResourceAttr("data.tcpshield_bedrock_tunnels.all", "tunnels.#", "1"),
					resource.TestCheckResourceAttr("data.tcpshield_bedrock_tunnel_locations.l", "locations.#", "3"),
				),
			},
			{
				Config: cfg("IAD", 1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_bedrock_tunnel.t", "location", "IAD"),
					resource.TestCheckResourceAttr("tcpshield_bedrock_tunnel.t", "proxy_protocol", "1"),
				),
			},
			liveImport("tcpshield_bedrock_tunnel.t", []string{"network_id"}),
		},
	})
}

func TestAccMockSentryTunnel(t *testing.T) {
	newMockTCPShield(t)
	cfg := func(name string, end int) string {
		return fmt.Sprintf(`
resource "tcpshield_sentry_tunnel" "t" {
  name          = %q
  endpoint_peer = "192.0.2.50"
  bind_port     = 40000
}
resource "tcpshield_sentry_tunnel_firewall_rule" "mc" {
  tunnel_id  = tcpshield_sentry_tunnel.t.id
  start_port = 25565
  end_port   = %d
  filter_id  = 9
}
data "tcpshield_sentry_tunnel_firewall_rules" "all" {
  tunnel_id  = tcpshield_sentry_tunnel.t.id
  depends_on = [tcpshield_sentry_tunnel_firewall_rule.mc]
}
data "tcpshield_sentry_tunnel_locations" "l" {}
data "tcpshield_sentry_tunnel_bind_port" "p" {}`, name, end)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg("sentry", 25565),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_sentry_tunnel.t", "location", "ANY"),
					resource.TestCheckResourceAttr("tcpshield_sentry_tunnel.t", "endpoint_public_key", "generated-public-key"),
					resource.TestCheckResourceAttr("tcpshield_sentry_tunnel.t", "tunnel_addresses.0.ip_address_public", "198.51.100.1"),
					resource.TestCheckResourceAttr("tcpshield_sentry_tunnel_firewall_rule.mc", "is_active", "true"),
					resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnel_firewall_rules.all", "rules.#", "1"),
					resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnel_locations.l", "locations.ANY", "Anycast"),
					resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnel_bind_port.p", "port", "40000"),
				),
			},
			{
				Config: cfg("sentry-renamed", 25570),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_sentry_tunnel.t", "name", "sentry-renamed"),
					resource.TestCheckResourceAttr("tcpshield_sentry_tunnel.t", "endpoint_public_key", "generated-public-key"),
					resource.TestCheckResourceAttr("tcpshield_sentry_tunnel_firewall_rule.mc", "end_port", "25570"),
				),
			},
			liveImport("tcpshield_sentry_tunnel.t", nil),
			liveImport("tcpshield_sentry_tunnel_firewall_rule.mc", []string{"tunnel_id"}),
		},
	})
}

func TestAccMockUserProfile(t *testing.T) {
	newMockTCPShield(t)
	cfg := func(lastname string) string {
		return fmt.Sprintf(`
data "tcpshield_user" "me" { depends_on = [tcpshield_user_profile.me] }
resource "tcpshield_user_profile" "me" {
  firstname = "Test"
  lastname  = %q
  phone     = "+49 30 0000000"
}`, lastname)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				// An empty field fails at plan time, before the API refuses it.
				Config:      strings.Replace(cfg("User"), `"+49 30 0000000"`, `""`, 1),
				ExpectError: regexp.MustCompile(`length must be at\s+least 1`),
			},
			{
				Config: cfg("Renamed"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.tcpshield_user.me", "plan_name", "Pro"),
					resource.TestCheckResourceAttr("data.tcpshield_user.me", "lastname", "Renamed"),
					resource.TestCheckResourceAttr("tcpshield_user_profile.me", "lastname", "Renamed"),
				),
			},
		},
	})
}

// TestAccMockReadOnlyDataSources covers the data sources no lifecycle test
// reaches: analytics, the single-entity tunnel reads and the tunnel helpers.
func TestAccMockReadOnlyDataSources(t *testing.T) {
	newMockTCPShield(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{{
			Config: `
resource "tcpshield_network" "n" { name = "n" }
resource "tcpshield_backend_set" "a" {
  network_id = tcpshield_network.n.id
  name       = "a"
  backends   = ["192.0.2.1:19132"]
}
resource "tcpshield_bedrock_tunnel" "t" {
  network_id     = tcpshield_network.n.id
  name           = "bedrock"
  backend_set_id = tcpshield_backend_set.a.id
  location       = "FRA"
}
resource "tcpshield_sentry_tunnel" "s" {
  name          = "sentry"
  endpoint_peer = "192.0.2.50"
  bind_port     = 40000
}

data "tcpshield_networks" "all" { depends_on = [tcpshield_network.n] }
data "tcpshield_analytics" "users" {
  network_id = tcpshield_network.n.id
  metric     = "uniqueUsers"
}
data "tcpshield_analytics" "top" {
  network_id = tcpshield_network.n.id
  metric     = "topDomains"
}
data "tcpshield_bedrock_tunnel" "t" {
  network_id = tcpshield_network.n.id
  id         = tcpshield_bedrock_tunnel.t.id
}
data "tcpshield_sentry_tunnel" "s" { id = tcpshield_sentry_tunnel.s.id }
data "tcpshield_sentry_tunnels" "all" { depends_on = [tcpshield_sentry_tunnel.s] }
data "tcpshield_sentry_tunnel_setup_script" "s" { tunnel_id = tcpshield_sentry_tunnel.s.id }
data "tcpshield_sentry_tunnel_analytics" "s" { tunnel_id = tcpshield_sentry_tunnel.s.id }
data "tcpshield_sentry_tunnel_filters" "all" {}
`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.tcpshield_networks.all", "networks.#", "1"),
				resource.TestCheckResourceAttr("data.tcpshield_analytics.users", "json", `{"value":42}`),
				resource.TestCheckResourceAttr("data.tcpshield_analytics.top", "json", `[{"key":"play.example.com","value":42}]`),
				resource.TestCheckResourceAttrPair("data.tcpshield_bedrock_tunnel.t", "allocated_cname", "tcpshield_bedrock_tunnel.t", "allocated_cname"),
				resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnel.s", "name", "sentry"),
				resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnel.s", "endpoint_private_key", "generated-private-key"),
				resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnels.all", "tunnels.#", "1"),
				resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnel_setup_script.s", "script", "#!/bin/bash\necho setup"),
				resource.TestCheckResourceAttrSet("data.tcpshield_sentry_tunnel_analytics.s", "json"),
				resource.TestCheckResourceAttr("data.tcpshield_sentry_tunnel_filters.all", "filters.0.internal_name", "mc"),
			),
		}},
	})
}
