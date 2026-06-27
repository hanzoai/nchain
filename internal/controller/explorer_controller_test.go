package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

func envMap(vars []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Name] = v.Value
	}
	return m
}

// TestBuildExplorerEnv verifies the Rust-parity env wiring: per-chain indexer
// refs, the default ref driving NETWORK_ID (resolved via Indexer -> Chain) and
// the read API host, plus branding and NEXT_PUBLIC_* UI config.
func TestBuildExplorerEnv(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	ns := "lux-mainnet"
	chain := &v1alpha1.Chain{
		ObjectMeta: metav1.ObjectMeta{Name: "hanzo-chain", Namespace: ns},
		Spec:       v1alpha1.ChainSpec{Protocol: "lux", ChainID: "36963", NodeClusterRef: "nc"},
	}
	indexer := &v1alpha1.Indexer{
		ObjectMeta: metav1.ObjectMeta{Name: "hanzo", Namespace: ns},
		Spec:       v1alpha1.IndexerSpec{ChainRef: "hanzo-chain", NodeClusterRef: "nc"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(chain, indexer).Build()
	r := &ExplorerReconciler{Client: c, Scheme: scheme}

	explorer := &v1alpha1.Explorer{
		ObjectMeta: metav1.ObjectMeta{Name: "lux", Namespace: ns},
		Spec: v1alpha1.ExplorerSpec{
			BackendImage: v1alpha1.ImageSpec{Repository: "ghcr.io/luxfi/explore"},
			ChainRef:     "hanzo-chain",
			IndexerRefs: []v1alpha1.ExplorerIndexerRef{
				{IndexerName: "lux-c", DisplayName: "Lux C-Chain"},
				{IndexerName: "hanzo", DisplayName: "Hanzo", Default: true},
			},
			Branding: &v1alpha1.ExplorerBranding{NetworkName: "Lux", LogoURL: "https://lux.network/logo.svg"},
			Ingress:  &v1alpha1.IngressSpec{Enabled: true, Hosts: []string{"explore.lux.network"}},
		},
	}

	got := envMap(r.buildExplorerEnv(context.Background(), explorer))
	want := map[string]string{
		"CHAIN_0_NAME":             "Lux C-Chain",
		"CHAIN_0_INDEXER":          "http://lux-c-indexer.lux-mainnet.svc:4000",
		"CHAIN_1_NAME":             "Hanzo",
		"CHAIN_1_INDEXER":          "http://hanzo-indexer.lux-mainnet.svc:4000",
		"CHAIN_COUNT":              "2",
		"NEXT_PUBLIC_APP_HOST":     "explore.lux.network",
		"NEXT_PUBLIC_NETWORK_NAME": "Lux",
		"NEXT_PUBLIC_NETWORK_ID":   "36963",
		"NEXT_PUBLIC_API_HOST":     "http://hanzo-indexer.lux-mainnet.svc:4000",
		"NEXT_PUBLIC_APP_PROTOCOL": "https",
		"NEXT_PUBLIC_API_PROTOCOL": "http",
		"NETWORK_NAME":             "Lux",
		"LOGO_URL":                 "https://lux.network/logo.svg",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, got[k], v)
		}
	}
}

// TestBuildExplorerEnvDefaults verifies the fallbacks when no indexer refs,
// branding, or ingress are supplied.
func TestBuildExplorerEnvDefaults(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &ExplorerReconciler{Client: c, Scheme: scheme}

	explorer := &v1alpha1.Explorer{
		ObjectMeta: metav1.ObjectMeta{Name: "lux", Namespace: "default"},
		Spec: v1alpha1.ExplorerSpec{
			BackendImage: v1alpha1.ImageSpec{Repository: "ghcr.io/luxfi/explore"},
			ChainRef:     "c",
		},
	}

	got := envMap(r.buildExplorerEnv(context.Background(), explorer))
	if got["CHAIN_COUNT"] != "0" {
		t.Errorf("CHAIN_COUNT = %q, want 0", got["CHAIN_COUNT"])
	}
	if got["NEXT_PUBLIC_NETWORK_ID"] != "1" {
		t.Errorf("NEXT_PUBLIC_NETWORK_ID = %q, want fallback 1", got["NEXT_PUBLIC_NETWORK_ID"])
	}
	if got["NEXT_PUBLIC_APP_HOST"] != "lux-explorer.default.svc" {
		t.Errorf("NEXT_PUBLIC_APP_HOST = %q, want service DNS fallback", got["NEXT_PUBLIC_APP_HOST"])
	}
	if _, ok := got["NETWORK_NAME"]; ok {
		t.Error("NETWORK_NAME should be absent without branding")
	}
}
