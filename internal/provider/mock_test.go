package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// mockTCPShield is an in-memory stand-in for the TCPShield API, faithful to the
// quirks the live API showed (2026-10):
//   - creates answer {"data": {"id": n}, "message": …, "status": 0};
//   - a single backend set read carries backends but no id, the listing the
//     reverse;
//   - backend set, domain and Bedrock tunnel PATCHes replace the whole entity;
//   - domain (pre)verification answers 403 for a domain whose DNS is not set up.
//
// Entities live in one id space; a collection path selects the entities of one
// parent, and an item path ends in the entity's id.
type mockTCPShield struct {
	mu       sync.Mutex
	profile  map[string]any
	nextID   int64
	items    map[int64]*mockItem
	requests []string
}

type mockItem struct {
	collection string
	attrs      map[string]any
}

var analyticsPath = regexp.MustCompile(`^/networks/\d+/analytics/(\w+)$`)

func newMockTCPShield(t *testing.T) *mockTCPShield {
	t.Helper()
	m := &mockTCPShield{nextID: 1000, items: map[int64]*mockItem{},
		profile: map[string]any{"firstname": "Test", "lastname": "User", "phone": "+49 30 0000000"}}
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	t.Setenv("TCPSHIELD_ENDPOINT", srv.URL)
	t.Setenv("TCPSHIELD_API_KEY", "test")
	accEnv(t)
	return m
}

// collectionKey canonicalises a collection path: the Sentry firewall is created
// under /tunnel/{id}/tunnelFirewall and listed under …/tunnelFirewalls.
func collectionKey(p string) string {
	return strings.TrimSuffix(p, "s") // networks → network, tunnelFirewalls → tunnelFirewall, …
}

func (m *mockTCPShield) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := r.URL.Path
	m.requests = append(m.requests, r.Method+" "+p)

	if r.Header.Get("X-API-Key") == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
		return
	}

	switch {
	case p == "/user/summary":
		data := map[string]any{"id": 1, "email": "test@example.com", "balance": "0", "plan_id": 2, "plan_name": "Pro", "commit_level": nil}
		for k, v := range m.profile {
			data[k] = v
		}
		writeJSON(w, 200, map[string]any{"status": 0, "data": data})
		return
	case p == "/user/updateGeneralInfo":
		// Like the real API: every field on every call, none empty.
		b := decodeBody(r)
		for _, k := range []string{"firstname", "lastname", "phone"} {
			if s, _ := b[k].(string); s == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]string{
					"UpdateUserGeneralSettings." + k: k + " is a required field"}})
				return
			}
		}
		m.profile = b
		writeJSON(w, 200, map[string]any{"status": 0, "message": "Info updated successfully."})
		return
	case strings.HasSuffix(p, "/domains/preverify"), strings.HasSuffix(p, "/verify"):
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Failed to verify domain.", "status": 1})
		return
	case strings.HasSuffix(p, "/bedrockTunnels/locations"):
		writeJSON(w, 200, []string{"FRA", "IAD", "ANY"})
		return
	case p == "/tunnels/locations":
		writeJSON(w, 200, map[string]string{"ANY": "Anycast"})
		return
	case p == "/tunnels/getBindPort":
		writeJSON(w, 200, 40000)
		return
	case p == "/tunnelFilters":
		writeJSON(w, 200, []map[string]any{
			{"id": 9, "internal_name": "mc", "human_name": "Minecraft Java", "created_at": "2025-03-11 06:35:52.412 +0000 UTC"},
			{"id": 10, "internal_name": "tcp", "human_name": "TCP Generic", "created_at": "2025-03-11 06:35:52.412 +0000 UTC"},
		})
		return
	case analyticsPath.MatchString(p):
		// Shapes per the spec; the live account had no analytics access.
		switch analyticsPath.FindStringSubmatch(p)[1] {
		case "bounceRate":
			writeJSON(w, 200, map[string]any{"value": 0.25})
		case "topDomains":
			writeJSON(w, 200, []map[string]any{{"key": "play.example.com", "value": 42}})
		case "uniqueUsers", "mitigatedCount":
			writeJSON(w, 200, map[string]any{"value": 42})
		default:
			writeJSON(w, 200, map[string]any{})
		}
		return
	case strings.HasPrefix(p, "/tunnels/") && strings.HasSuffix(p, "/setupScript"):
		writeJSON(w, 200, map[string]any{"status": 0, "data": "#!/bin/bash\necho setup"})
		return
	case strings.HasPrefix(p, "/tunnels/") && strings.HasSuffix(p, "/analytics"):
		writeJSON(w, 200, map[string]any{
			"ingress": []map[string]any{{"value": 1.5, "time": "2026-10-11T00:00:00Z"}},
			"egress":  []map[string]any{{"value": 0.5, "time": "2026-10-11T00:00:00Z"}},
			"max":     1.5,
		})
		return
	}

	segs := strings.Split(strings.Trim(p, "/"), "/")
	id, err := strconv.ParseInt(segs[len(segs)-1], 10, 64)
	if err != nil { // collection
		key := collectionKey(p)
		switch r.Method {
		case http.MethodGet:
			list := []map[string]any{}
			for iid, it := range m.items {
				if it.collection == key {
					list = append(list, m.view(iid, it, true))
				}
			}
			writeJSON(w, 200, list)
		case http.MethodPost:
			m.nextID++
			attrs := defaultsFor(key, m.nextID)
			for k, v := range decodeBody(r) {
				attrs[k] = v
			}
			m.items[m.nextID] = &mockItem{collection: key, attrs: attrs}
			writeJSON(w, 200, map[string]any{"status": 0, "message": "created", "data": map[string]any{"id": m.nextID}})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	it, ok := m.items[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Not found"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, m.view(id, it, false))
	case http.MethodPatch, http.MethodPut:
		b := decodeBody(r)
		if replaces(it.collection) {
			fresh := defaultsFor(it.collection, id)
			for k, v := range it.attrs {
				if isComputed(k) {
					fresh[k] = v
				}
			}
			it.attrs = fresh
		}
		for k, v := range b {
			it.attrs[k] = v
		}
		writeJSON(w, 200, map[string]any{"status": 0, "message": "updated"})
	case http.MethodDelete:
		delete(m.items, id)
		// Deleting a parent drops its children, as the real API does.
		for cid, c := range m.items {
			if strings.Contains(c.collection, fmt.Sprintf("/%d/", id)) {
				delete(m.items, cid)
			}
		}
		writeJSON(w, 200, map[string]any{"status": 0, "message": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// view renders an entity the way the real API does for a list or a single read.
func (m *mockTCPShield) view(id int64, it *mockItem, inList bool) map[string]any {
	out := map[string]any{"id": id}
	for k, v := range it.attrs {
		out[k] = v
	}
	if strings.HasSuffix(it.collection, "/backendSet") {
		if inList {
			delete(out, "backends")
		} else {
			delete(out, "id")
		}
	}
	return out
}

// replaces reports whether a PATCH on the collection's entities behaves as a PUT.
func replaces(collection string) bool {
	for _, s := range []string{"/backendSet", "/domain", "/bedrockTunnel"} {
		if strings.HasSuffix(collection, s) {
			return true
		}
	}
	return false
}

func isComputed(k string) bool {
	switch k {
	case "created_at", "verified", "allocated_cname", "country_code", "country_name":
		return true
	}
	return false
}

func defaultsFor(collection string, id int64) map[string]any {
	d := map[string]any{"created_at": "2026-10-11T00:00:00Z"}
	switch {
	case collection == "/network":
		d["premium"] = false
		d["protected_cname"] = fmt.Sprintf("%d.ipv4.tcpshield.com", id)
		d["txt_verification"] = "tcpshield-network-verification=TEST"
		d["connections_per_second_threshold"] = 5
		d["client_ban_seconds"] = 60
		d["client_allow_seconds"] = 600
		d["mitigation_message"] = "verified"
		for _, k := range []string{"vpn_block", "attack_emails", "verification_server", "forward_true_slp", "offline_motd_enabled"} {
			d[k] = false
		}
		for _, k := range []string{"webhook_url", "antivpn_kick_msg", "offline_motd", "offline_kick_message", "offline_favicon"} {
			d[k] = ""
		}
	case strings.HasSuffix(collection, "/backendSet"):
		d["proxy_protocol"] = false
		d["vulcan_ac_enabled"] = false
		d["load_balancing_mode"] = 0
	case strings.HasSuffix(collection, "/domain"):
		d["bac"] = false
		d["verified"] = false
	case strings.HasSuffix(collection, "/countryFirewall"):
		d["country_code"] = "AD"
		d["country_name"] = "Andorra"
	case strings.HasSuffix(collection, "/bedrockTunnel"):
		d["proxy_protocol"] = 0
		d["allocated_cname"] = fmt.Sprintf("%d.bedrock.tcpshield.com", id)
	case collection == "/tunnel":
		d["location"] = "ANY"
		d["endpoint_public_key"] = "generated-public-key"
		d["endpoint_private_key"] = "generated-private-key"
		d["tunnel_addresses"] = []map[string]any{{
			"ip_address_public": "198.51.100.1", "ip_address_private": "10.0.0.1",
			"peer_ip_address_private": "10.0.0.2", "location": "ANY",
		}}
	case strings.HasSuffix(collection, "/tunnelFirewall"):
		d["is_active"] = true
		d["internal_name"] = "mc"
		d["human_name"] = "Minecraft Java"
	}
	return d
}

func decodeBody(r *http.Request) map[string]any {
	out := map[string]any{}
	_ = json.NewDecoder(r.Body).Decode(&out)
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
