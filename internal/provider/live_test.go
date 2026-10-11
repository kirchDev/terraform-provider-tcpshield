package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"text/template"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/kirchDev/terraform-provider-tcpshield/internal/client"
)

// The live tests run against the real TCPShield API and create, change and
// destroy real entities — a whole network included. They never run by
// accident: besides TF_ACC they need TCPSHIELD_LIVE=1 and a TCPSHIELD_API_KEY
// whose account has a free network slot.
//
//	TF_ACC=1 TCPSHIELD_LIVE=1 TCPSHIELD_API_KEY=… go test ./internal/provider -run TestLive -v
//
// Bedrock and Sentry tunnels are not covered: the Pro plan allows no Bedrock
// tunnel, and Sentry tunnel creation failed server-side on the test account.
// Their CRUD glue is covered by the mock acceptance tests instead.
func liveEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("TCPSHIELD_LIVE") != "1" || os.Getenv("TCPSHIELD_API_KEY") == "" {
		t.Skip("set TCPSHIELD_LIVE=1 and TCPSHIELD_API_KEY to run live tests against the real API")
	}
	accEnv(t)
}

func liveClient() *client.Client {
	return client.New(os.Getenv("TCPSHIELD_ENDPOINT"), os.Getenv("TCPSHIELD_API_KEY"), "terraform-provider-tcpshield/live-test")
}

func TestLiveNetworkLifecycle(t *testing.T) {
	liveEnv(t)
	suffix := acctest.RandString(8)
	name := "tf-acc-" + suffix
	domain := "tf-acc-" + suffix + ".invalid"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		CheckDestroy:             liveCheckNetworkDestroyed(name),
		Steps: []resource.TestStep{
			{
				Config: liveConfig(name, domain, liveV1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_network.n", "name", name),
					resource.TestCheckResourceAttr("tcpshield_network.n", "client_ban_seconds", "120"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "vpn_block", "true"),
					resource.TestCheckResourceAttrSet("tcpshield_network.n", "protected_cname"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "backends.#", "2"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "proxy_protocol", "true"),
					resource.TestCheckResourceAttr("tcpshield_domain.d", "name", domain),
					resource.TestCheckResourceAttr("tcpshield_domain.d", "verified", "false"),
					resource.TestCheckResourceAttr("tcpshield_firewall_rule.ip", "cidr", "192.0.2.0/24"),
					resource.TestCheckResourceAttr("tcpshield_asn_firewall_rule.asn", "asn", "64496"),
					resource.TestCheckResourceAttr("tcpshield_country_firewall_rule.c", "country_code", "AD"),
					resource.TestCheckResourceAttr("data.tcpshield_domain_verification.v", "verified", "false"),
					resource.TestCheckResourceAttr("data.tcpshield_domains.all", "domains.#", "1"),
					resource.TestCheckResourceAttr("data.tcpshield_backend_sets.all", "backend_sets.#", "2"),
					resource.TestCheckResourceAttr("data.tcpshield_backend_sets.all", "backend_sets.0.backends.#", "2"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "connections_per_second_threshold", "10"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "client_allow_seconds", "300"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "mitigation_message", "tf-acc mitigation"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "attack_emails", "true"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "forward_true_slp", "true"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "offline_kick_message", "tf-acc kick"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "offline_favicon", "data:image/png;base64,AAAA"),
					resource.TestCheckResourceAttr("data.tcpshield_domain_preverify.p", "verified", "false"),
					resource.TestCheckResourceAttrPair("data.tcpshield_network.by_id", "name", "tcpshield_network.n", "name"),
					resource.TestCheckResourceAttrPair("data.tcpshield_network.by_id", "protected_cname", "tcpshield_network.n", "protected_cname"),
					resource.TestCheckResourceAttrPair("data.tcpshield_domain.by_id", "name", "tcpshield_domain.d", "name"),
					resource.TestCheckResourceAttrPair("data.tcpshield_domain.by_name", "id", "tcpshield_domain.d", "id"),
					resource.TestCheckResourceAttr("data.tcpshield_backend_set.a", "backends.#", "2"),
					resource.TestCheckResourceAttr("data.tcpshield_backend_set.a", "proxy_protocol", "true"),
					resource.TestCheckResourceAttr("data.tcpshield_firewall_rules.ip", "rules.0.cidr", "192.0.2.0/24"),
					resource.TestCheckResourceAttr("data.tcpshield_asn_firewall_rules.asn", "rules.0.asn", "64496"),
					resource.TestCheckResourceAttr("data.tcpshield_country_firewall_rules.c", "rules.0.country_code", "AD"),
				),
			},
			{
				// Every setting moves; the domain moves to the second backend set,
				// and the IP entry flips to a whitelist entry (a replacement).
				Config: liveConfig(name, domain, liveV2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_network.n", "client_ban_seconds", "300"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "vpn_block", "false"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "offline_motd", ""),
					resource.TestCheckResourceAttr("tcpshield_network.n", "offline_kick_message", ""),
					resource.TestCheckResourceAttr("tcpshield_network.n", "offline_favicon", ""),
					resource.TestCheckResourceAttr("tcpshield_network.n", "connections_per_second_threshold", "20"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "client_allow_seconds", "600"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "mitigation_message", "tf-acc mitigation 2"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "antivpn_kick_msg", "tf-acc vpn 2"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "attack_emails", "false"),
					resource.TestCheckResourceAttr("tcpshield_network.n", "forward_true_slp", "false"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "vulcan_ac_enabled", "true"),
					resource.TestCheckResourceAttr("data.tcpshield_backend_set.a", "vulcan_ac_enabled", "true"),
					resource.TestCheckResourceAttr("data.tcpshield_firewall_rules.ip", "rules.0.whitelist", "true"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "backends.#", "1"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "proxy_protocol", "false"),
					resource.TestCheckResourceAttr("tcpshield_backend_set.a", "load_balancing_mode", "1"),
					resource.TestCheckResourceAttr("tcpshield_domain.d", "bac", "true"),
					resource.TestCheckResourceAttrPair("tcpshield_domain.d", "backend_set_id", "tcpshield_backend_set.b", "id"),
					resource.TestCheckResourceAttr("tcpshield_firewall_rule.ip", "whitelist", "true"),
				),
			},
			liveImport("tcpshield_network.n", nil),
			liveImport("tcpshield_backend_set.a", []string{"network_id"}),
			liveImport("tcpshield_domain.d", []string{"network_id"}),
			liveImport("tcpshield_firewall_rule.ip", []string{"network_id"}),
			liveImport("tcpshield_asn_firewall_rule.asn", []string{"network_id"}),
			liveImport("tcpshield_country_firewall_rule.c", []string{"network_id"}),
		},
	})
}

// liveImport imports a resource by its "parent/id" import id and compares the
// result with state.
func liveImport(addr string, parents []string) resource.TestStep {
	return resource.TestStep{
		ResourceName:      addr,
		ImportState:       true,
		ImportStateVerify: true,
		ImportStateIdFunc: func(s *terraform.State) (string, error) {
			rs, ok := s.RootModule().Resources[addr]
			if !ok {
				return "", fmt.Errorf("%s not in state", addr)
			}
			id := ""
			for _, p := range parents {
				id += rs.Primary.Attributes[p] + "/"
			}
			return id + rs.Primary.Attributes["id"], nil
		},
	}
}

func liveCheckNetworkDestroyed(name string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		var list []apiNetwork
		if err := liveClient().Get(context.Background(), "/networks", &list); err != nil {
			return err
		}
		for _, n := range list {
			if n.Name == name {
				return fmt.Errorf("network %s (%d) still exists after destroy", name, n.ID)
			}
		}
		return nil
	}
}

// liveVals are the values one step of the lifecycle test configures. Every
// setting moves between liveV1 and liveV2, so each one is written by an update
// at least once.
type liveVals struct {
	Name, Domain                 string
	CPS, Ban, Allow, LB          int
	Vpn, Emails, SLP, PP, Vulcan bool
	BAC, Whitelist               bool
	Mitigation, VpnMsg           string
	Motd, Kick, Favicon          string
	Backends                     string
	DomainSet                    string
}

var (
	liveV1 = liveVals{CPS: 10, Ban: 120, Allow: 300, LB: 0, Vpn: true, Emails: true, SLP: true, PP: true, Vulcan: false,
		BAC: false, Whitelist: false, Mitigation: "tf-acc mitigation", VpnMsg: "tf-acc vpn",
		Motd: "tf-acc offline", Kick: "tf-acc kick", Favicon: "data:image/png;base64,AAAA",
		Backends: `["192.0.2.10:25565", "192.0.2.11:25565"]`, DomainSet: "a"}
	liveV2 = liveVals{CPS: 20, Ban: 300, Allow: 600, LB: 1, Vpn: false, Emails: false, SLP: false, PP: false, Vulcan: true,
		BAC: true, Whitelist: true, Mitigation: "tf-acc mitigation 2", VpnMsg: "tf-acc vpn 2",
		Motd: "", Kick: "", Favicon: "",
		Backends: `["192.0.2.12:25565"]`, DomainSet: "b"}
)

var liveTemplate = template.Must(template.New("live").Parse(`
resource "tcpshield_network" "n" {
  name                             = "{{.Name}}"
  connections_per_second_threshold = {{.CPS}}
  client_ban_seconds               = {{.Ban}}
  client_allow_seconds             = {{.Allow}}
  mitigation_message               = "{{.Mitigation}}"
  vpn_block                        = {{.Vpn}}
  antivpn_kick_msg                 = "{{.VpnMsg}}"
  attack_emails                    = {{.Emails}}
  forward_true_slp                 = {{.SLP}}
  offline_motd_enabled             = {{.Vpn}}
  offline_motd                     = "{{.Motd}}"
  offline_kick_message             = "{{.Kick}}"
  offline_favicon                  = "{{.Favicon}}"
}

resource "tcpshield_backend_set" "a" {
  network_id          = tcpshield_network.n.id
  name                = "tf-acc-a"
  backends            = {{.Backends}}
  proxy_protocol      = {{.PP}}
  vulcan_ac_enabled   = {{.Vulcan}}
  load_balancing_mode = {{.LB}}
}

resource "tcpshield_backend_set" "b" {
  network_id = tcpshield_network.n.id
  name       = "tf-acc-b"
  backends   = ["192.0.2.20:25565", "192.0.2.21:25565"]
}

resource "tcpshield_domain" "d" {
  network_id     = tcpshield_network.n.id
  name           = "{{.Domain}}"
  backend_set_id = tcpshield_backend_set.{{.DomainSet}}.id
  bac            = {{.BAC}}
}

resource "tcpshield_firewall_rule" "ip" {
  network_id = tcpshield_network.n.id
  cidr       = "192.0.2.0/24"
  whitelist  = {{.Whitelist}}
}

resource "tcpshield_asn_firewall_rule" "asn" {
  network_id = tcpshield_network.n.id
  asn        = 64496
}

resource "tcpshield_country_firewall_rule" "c" {
  network_id = tcpshield_network.n.id
  country_id = 1
}

data "tcpshield_domain_verification" "v" {
  network_id = tcpshield_network.n.id
  domain_id  = tcpshield_domain.d.id
}

data "tcpshield_domain_preverify" "p" {
  network_id  = tcpshield_network.n.id
  domain_name = "{{.Domain}}"
}

data "tcpshield_network" "by_id" {
  id         = tcpshield_network.n.id
  depends_on = [tcpshield_network.n]
}

data "tcpshield_domain" "by_id" {
  network_id = tcpshield_network.n.id
  id         = tcpshield_domain.d.id
  depends_on = [tcpshield_domain.d]
}

data "tcpshield_domain" "by_name" {
  network_id = tcpshield_network.n.id
  name       = tcpshield_domain.d.name
  depends_on = [tcpshield_domain.d]
}

data "tcpshield_backend_set" "a" {
  network_id = tcpshield_network.n.id
  id         = tcpshield_backend_set.a.id
  depends_on = [tcpshield_backend_set.a]
}

data "tcpshield_domains" "all" {
  network_id = tcpshield_network.n.id
  depends_on = [tcpshield_domain.d]
}

data "tcpshield_backend_sets" "all" {
  network_id = tcpshield_network.n.id
  depends_on = [tcpshield_backend_set.a, tcpshield_backend_set.b]
}

data "tcpshield_firewall_rules" "ip" {
  network_id = tcpshield_network.n.id
  depends_on = [tcpshield_firewall_rule.ip]
}

data "tcpshield_asn_firewall_rules" "asn" {
  network_id = tcpshield_network.n.id
  depends_on = [tcpshield_asn_firewall_rule.asn]
}

data "tcpshield_country_firewall_rules" "c" {
  network_id = tcpshield_network.n.id
  depends_on = [tcpshield_country_firewall_rule.c]
}
`))

func liveConfig(name, domain string, v liveVals) string {
	v.Name, v.Domain = name, domain
	var b strings.Builder
	if err := liveTemplate.Execute(&b, v); err != nil {
		panic(err)
	}
	return b.String()
}

// TestLiveUserProfile changes the account's last name and puts it back. The
// phone stays as it is: the API refuses an empty phone, so a number the test
// set could never be removed again. A cleanup restores the original values even
// when a step fails.
func TestLiveUserProfile(t *testing.T) {
	liveEnv(t)
	c := liveClient()
	orig, err := readUser(context.Background(), c)
	if err != nil {
		t.Fatalf("reading user summary: %v", err)
	}
	if orig.Phone == "" {
		t.Skip("the account has no phone number; the API refuses a profile update without one")
	}
	t.Cleanup(func() {
		_, err := c.Write(context.Background(), "POST", "/user/updateGeneralInfo",
			map[string]string{"firstname": orig.Firstname, "lastname": orig.Lastname, "phone": orig.Phone}, nil)
		if err != nil {
			t.Errorf("restoring the original profile: %v", err)
		}
	})

	cfg := func(lastname, phone string) string {
		return fmt.Sprintf(`
resource "tcpshield_user_profile" "me" {
  firstname = %q
  lastname  = %q
  phone     = %q
}`, orig.Firstname, lastname, phone)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg(orig.Lastname+"-tfacc", orig.Phone),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_user_profile.me", "lastname", orig.Lastname+"-tfacc"),
					resource.TestCheckResourceAttr("tcpshield_user_profile.me", "phone", orig.Phone),
				),
			},
			{
				Config: cfg(orig.Lastname, orig.Phone),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("tcpshield_user_profile.me", "lastname", orig.Lastname),
					resource.TestCheckResourceAttr("tcpshield_user_profile.me", "phone", orig.Phone),
				),
			},
			{
				ResourceName:      "tcpshield_user_profile.me",
				ImportState:       true,
				ImportStateId:     "me",
				ImportStateVerify: true,
			},
		},
	})
}
