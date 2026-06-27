package controller

import (
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

func newGateway(spec v1alpha1.GatewaySpec) *v1alpha1.Gateway {
	return &v1alpha1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "lux"},
		Spec:       spec,
	}
}

// parseConfig unmarshals the rendered gateway config and fails the test on error.
func parseConfig(t *testing.T, cfg string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(cfg), &out); err != nil {
		t.Fatalf("rendered config is not valid JSON: %v\n%s", err, cfg)
	}
	return out
}

func TestBuildGatewayConfigLuxAutoRoutes(t *testing.T) {
	gw := newGateway(v1alpha1.GatewaySpec{
		NodeClusterRef: "validators",
		AutoRoutes:     true,
		RateLimits:     &v1alpha1.RateLimitConfig{MaxRate: 5000, Every: "1s", ClientMaxRate: 100},
	})
	backend := "http://validators-headless.lux.svc:8545"
	chains := []chainRoute{{name: "hanzo", id: "abc123"}, {name: "zoo", id: "def456"}}

	cfg, count := buildGatewayConfig(gw, "lux", backend, chains)

	// 6 base routes + 2 routes (rpc + ws) per subnet chain.
	if want := 6 + 2*len(chains); count != want {
		t.Fatalf("route count = %d, want %d", count, want)
	}

	root := parseConfig(t, cfg)
	if root["version"].(float64) != 3 {
		t.Errorf("version = %v, want 3", root["version"])
	}
	for _, must := range []string{
		"/ext/bc/C/rpc", "/ext/bc/C/ws", "/ext/bc/X", "/ext/bc/P", "/ext/info", "/ext/health",
		"/ext/bc/abc123/rpc", "/ext/bc/abc123/ws", "/ext/bc/def456/rpc", "/ext/bc/def456/ws",
	} {
		if !strings.Contains(cfg, `"endpoint": "`+must+`"`) {
			t.Errorf("config missing endpoint %q", must)
		}
	}

	// CORS + service-level rate limit live in top-level extra_config.
	extra := root["extra_config"].(map[string]any)
	if _, ok := extra["security/cors"]; !ok {
		t.Error("missing security/cors")
	}
	if _, ok := extra["qos/ratelimit/service"]; !ok {
		t.Error("missing qos/ratelimit/service")
	}

	// The C-chain RPC endpoint must carry an endpoint-level rate limit.
	if !strings.Contains(cfg, `"qos/ratelimit/router"`) {
		t.Error("missing per-endpoint qos/ratelimit/router")
	}
	if !strings.Contains(cfg, `"strategy": "ip"`) {
		t.Error("client rate limit should set strategy ip")
	}
}

func TestBuildGatewayConfigDeterministic(t *testing.T) {
	gw := newGateway(v1alpha1.GatewaySpec{NodeClusterRef: "v", AutoRoutes: true})
	backend := "http://v-headless.lux.svc:8545"
	chains := []chainRoute{{name: "a", id: "1"}, {name: "b", id: "2"}}

	first, _ := buildGatewayConfig(gw, "lux", backend, chains)
	second, _ := buildGatewayConfig(gw, "lux", backend, chains)
	if first != second {
		t.Error("config rendering is not deterministic")
	}
}

func TestBuildGatewayConfigCustomAndIndexer(t *testing.T) {
	gw := newGateway(v1alpha1.GatewaySpec{
		NodeClusterRef: "v",
		AutoRoutes:     false,
		IndexerRefs:    []string{"idx-c"},
		Routes: []v1alpha1.GatewayRoute{
			{Prefix: "/custom", Backend: "svc.lux.svc:9000", Methods: []string{"POST"}},
		},
	})

	cfg, count := buildGatewayConfig(gw, "lux", "http://v-headless.lux.svc:8545", nil)
	if count != 2 {
		t.Fatalf("route count = %d, want 2 (indexer + custom)", count)
	}
	if !strings.Contains(cfg, `"endpoint": "/api/indexer/idx-c"`) {
		t.Error("missing indexer route")
	}
	if !strings.Contains(cfg, `"endpoint": "/custom"`) {
		t.Error("missing custom route")
	}
	// Bare backend hosts must be normalized to a URL.
	if !strings.Contains(cfg, `"http://svc.lux.svc:9000"`) {
		t.Error("custom backend was not given an http scheme")
	}
	parseConfig(t, cfg)
}

func TestBuildGatewayConfigNonLuxGeneric(t *testing.T) {
	gw := newGateway(v1alpha1.GatewaySpec{NodeClusterRef: "eth", AutoRoutes: true})
	cfg, count := buildGatewayConfig(gw, "ethereum", "http://eth-headless.lux.svc:8545", nil)
	if count != 1 {
		t.Fatalf("route count = %d, want 1 generic passthrough", count)
	}
	if !strings.Contains(cfg, `"endpoint": "/rpc"`) {
		t.Error("missing generic /rpc passthrough for non-lux protocol")
	}
}

func TestCORSConfigDefaultsAndOverride(t *testing.T) {
	def := corsConfig(nil)
	if got := def["allow_origins"].([]string); len(got) != 1 || got[0] != "*" {
		t.Errorf("default origins = %v, want [*]", got)
	}

	custom := corsConfig(&v1alpha1.GatewayCORS{AllowedOrigins: []string{"https://lux.network"}})
	if got := custom["allow_origins"].([]string); len(got) != 1 || got[0] != "https://lux.network" {
		t.Errorf("override origins = %v", got)
	}
	// Unset fields fall back to defaults.
	if got := custom["allow_methods"].([]string); len(got) == 0 {
		t.Error("methods should default when unset")
	}
}

func TestEffectiveRate(t *testing.T) {
	cases := []struct {
		every string
		max   int32
		want  float64
	}{
		{"1s", 5000, 5000},
		{"", 100, 100},
		{"1m", 6000, 100},
		{"bogus", 50, 50},
	}
	for _, c := range cases {
		got := effectiveRate(&v1alpha1.RateLimitConfig{MaxRate: c.max, Every: c.every})
		if got != c.want {
			t.Errorf("effectiveRate(every=%q,max=%d) = %v, want %v", c.every, c.max, got, c.want)
		}
	}
}

func TestEnsureScheme(t *testing.T) {
	cases := map[string]string{
		"svc.ns.svc:9000":       "http://svc.ns.svc:9000",
		"http://svc:80":         "http://svc:80",
		"https://api.lux.netwk": "https://api.lux.netwk",
	}
	for in, want := range cases {
		if got := ensureScheme(in); got != want {
			t.Errorf("ensureScheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClusterRPCPort(t *testing.T) {
	def := &v1alpha1.NodeCluster{}
	if got := clusterRPCPort(def); got != defaultClusterRPCPort {
		t.Errorf("default RPC port = %d, want %d", got, defaultClusterRPCPort)
	}
	custom := &v1alpha1.NodeCluster{Spec: v1alpha1.NodeClusterSpec{Ports: v1alpha1.PortConfig{RPC: 9650}}}
	if got := clusterRPCPort(custom); got != 9650 {
		t.Errorf("custom RPC port = %d, want 9650", got)
	}
}

func TestExternalEndpoint(t *testing.T) {
	gw := newGateway(v1alpha1.GatewaySpec{
		Ingress: &v1alpha1.IngressSpec{Enabled: true, TLS: true, Hosts: []string{"api.lux.network"}},
	})
	if got := externalEndpoint(gw); got != "https://api.lux.network" {
		t.Errorf("externalEndpoint = %q, want https://api.lux.network", got)
	}
	if got := externalEndpoint(newGateway(v1alpha1.GatewaySpec{})); got != "" {
		t.Errorf("externalEndpoint without ingress = %q, want empty", got)
	}
}
