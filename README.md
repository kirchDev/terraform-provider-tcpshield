<div align="center">

# 🛡️ terraform-provider-tcpshield

**Manage TCPShield as code — networks, domains, backend sets, firewalls, Bedrock and Sentry tunnels, reconciled by OpenTofu**

[![Release](https://img.shields.io/github/v/release/kirchDev/terraform-provider-tcpshield?style=flat-square&label=release&color=1E88E5)](https://github.com/kirchDev/terraform-provider-tcpshield/releases/latest)
[![OpenTofu Registry](https://img.shields.io/badge/opentofu-kirchdev%2Ftcpshield-FFDA18?style=flat-square&logo=opentofu&logoColor=black)](https://search.opentofu.org/provider/kirchdev/tcpshield/latest)
[![Terraform Registry](https://img.shields.io/badge/terraform-kirchdev%2Ftcpshield-7b42bc?style=flat-square&logo=terraform&logoColor=white)](https://registry.terraform.io/providers/kirchDev/tcpshield/latest)
[![Tests](https://img.shields.io/github/actions/workflow/status/kirchDev/terraform-provider-tcpshield/ci.yml?branch=main&style=flat-square&label=tests)](https://github.com/kirchDev/terraform-provider-tcpshield/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/github/license/kirchDev/terraform-provider-tcpshield?style=flat-square&color=1E88E5)](LICENSE)

</div>

---

```hcl
resource "tcpshield_domain" "play" {
  network_id     = tcpshield_network.main.id
  name           = "play.example.com"
  backend_set_id = tcpshield_backend_set.lobby.id
}
```

Your DDoS protection declared in HCL and reconciled by OpenTofu — not clicked together in the TCPShield panel. Covers the complete [TCPShield REST API](https://swagger.tcpshield.com/).

> [!IMPORTANT]
> **Pre-1.0.** Most of the provider is verified against the live API; some parts could not be, because the test account's plan does not include them. They are built from the official spec and covered by mock tests only — see [Test status](#-test-status). Pin an exact version.

## 📦 Install & run

```hcl
terraform {
  required_providers {
    tcpshield = {
      source  = "kirchdev/tcpshield"
      version = "~> 0.1"
    }
  }
}

provider "tcpshield" {
  api_key = var.tcpshield_api_key # or set TCPSHIELD_API_KEY
}

resource "tcpshield_network" "main" {
  name      = "play.example.com"
  vpn_block = true
}

resource "tcpshield_backend_set" "lobby" {
  network_id = tcpshield_network.main.id
  name       = "lobby"
  backends   = ["203.0.113.10:25565"]
}
```

```bash
export TCPSHIELD_API_KEY="..."   # TCPShield panel → Account → API
tofu plan
```

## ✨ Features

- **🛡️ Complete API** — every endpoint of the official spec is a resource or a data source, or deliberately left out (see below).
- **🔍 Verified against the live API** — the spec is loose about shapes; the provider follows what the API actually does (PATCHes that behave like PUTs, reads without ids, zero values silently ignored) and guards those traps with plan-time validation.
- **🧭 Import everything** — `tofu import` with `<network_id>/<id>` (or `<tunnel_id>/<id>`), every resource round-trips.
- **🎯 Only real changes** — Optional + Computed settings keep their live value when left out of the configuration.
- **🚀 OpenTofu & Terraform** — published as `kirchdev/tcpshield` on both registries.

## 🗺️ Coverage

<details>
<summary>Resources & data sources</summary>

- **Networks** — `tcpshield_network`; data sources `tcpshield_network(s)`, `tcpshield_analytics` (needs analytics on the account).
- **Backends & domains** — `tcpshield_backend_set`, `tcpshield_domain`; data sources `tcpshield_backend_set(s)`, `tcpshield_domain(s)`, `tcpshield_domain_preverify`, `tcpshield_domain_verification`.
- **Firewalls** — `tcpshield_firewall_rule` (CIDR), `tcpshield_asn_firewall_rule`, `tcpshield_country_firewall_rule`; each with a list data source.
- **Bedrock tunnels** — `tcpshield_bedrock_tunnel`; data sources `tcpshield_bedrock_tunnel(s)`, `tcpshield_bedrock_tunnel_locations`.
- **Sentry tunnels** — `tcpshield_sentry_tunnel`, `tcpshield_sentry_tunnel_firewall_rule`; data sources `tcpshield_sentry_tunnel(s)`, `…_locations`, `…_bind_port`, `…_filters`, `…_firewall_rules`, `…_setup_script`, `…_analytics`.
- **Account** — `tcpshield_user_profile` (name, phone); data source `tcpshield_user`.

</details>

<details>
<summary>Deliberately left out</summary>

- **`PATCH /user/email`, `POST /user/password`** — login credentials do not belong in a state file.
- **`GET /user/apikey`** — the provider already holds the key; exposing it again only copies a secret into state.
- **`POST /user/apikey/regenerate`** — would invalidate the provider's own credential in the middle of an apply.

</details>

## 🧪 Test status

Every resource and data source runs through acceptance tests against an in-memory mock of the API in CI. On top of that, the live suite runs against the real TCPShield API (Pro plan, 2026-10):

| Area | Live-verified | Notes |
| :-- | :-: | :-- |
| `tcpshield_network` (every setting), `tcpshield_backend_set`, `tcpshield_domain` | ✅ | create → update → import → destroy |
| `tcpshield_firewall_rule`, `tcpshield_asn_firewall_rule`, `tcpshield_country_firewall_rule` | ✅ | incl. whitelist flip (replacement) |
| `tcpshield_user_profile` | ✅ | rename and restore |
| All network, domain, backend set and firewall data sources, `tcpshield_domain_preverify`, `tcpshield_domain_verification`, `tcpshield_user` | ✅ | |
| `tcpshield_sentry_tunnels`, `…_locations`, `…_bind_port`, `…_filters`, `tcpshield_bedrock_tunnel_locations` | ✅ | read only |
| `tcpshield_network.webhook_url` | ⚠️ | only the rejection of a non-Discord/Slack URL is verified |
| `tcpshield_network.verification_server` | ⚠️ | Enterprise only; only the refusal on Pro is verified |
| `tcpshield_bedrock_tunnel`, data sources `tcpshield_bedrock_tunnel(s)` | ❌ **untested** | needs Geyser support (Premium or Enterprise) |
| `tcpshield_analytics` | ❌ **untested** | needs advanced analytics (Premium or Enterprise); only the 403 on Pro is verified |
| `tcpshield_sentry_tunnel`, `tcpshield_sentry_tunnel_firewall_rule`, data sources `tcpshield_sentry_tunnel`, `…_setup_script`, `…_analytics`, `…_firewall_rules` | ❌ **untested** | creating a tunnel failed server-side on the test account (`Allocating not used IP address failed.`) |

The untested parts follow the [OpenAPI spec](https://swagger.tcpshield.com/) and the behaviour of the verified endpoints. Reports from accounts that can use them are very welcome.

## 📚 Documentation

Per-resource docs live under [`docs/`](docs/), generated from the schema with `make docs` (build + export schema + tfplugindocs).

## 🤝 Contributing

PRs welcome. Conventional Commits required (enforced via commitlint). Husky runs the linters/formatters on `git commit`.

> [!TIP]
> `make test` and `TF_ACC=1 go test ./...` (mock acceptance tests, needs `tofu` on PATH) run without an account. The live suite needs one with a free network slot: `TF_ACC=1 TCPSHIELD_LIVE=1 TCPSHIELD_API_KEY=… go test ./internal/provider -run TestLive -v`.

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow.

## 🛣️ Versioning

[Semantic Versioning](https://semver.org/) via [release-please](https://github.com/googleapis/release-please) — see [CHANGELOG.md](CHANGELOG.md).

## 📄 License

[MIT](LICENSE) © [Titus Kirch](https://github.com/TitusKirch/) / [IT-Dienstleistungen Titus Kirch](https://kirch.dev)
