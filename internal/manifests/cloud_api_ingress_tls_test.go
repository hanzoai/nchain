package manifests

import (
	"testing"

	v1alpha1 "github.com/hanzoai/nchain/api/v1alpha1"
)

// A brand that cannot answer its HTTP-01 challenge must not be able to block
// certificate issuance for every other brand. That happens when all hosts share
// one SAN certificate, so the API Ingress must emit one TLS entry per brand.
func TestBuildCloudAPIIngress_TLSIsPerBrand(t *testing.T) {
	c := &v1alpha1.Cloud{}
	c.Name = "web3-platform"
	c.Namespace = "bootnode"
	c.Spec.Ingress = &v1alpha1.IngressSpec{Enabled: true, TLS: true}
	c.Spec.Brands = []v1alpha1.BrandConfig{
		{Name: "hanzo", Domain: "web3.hanzo.ai"},
		{Name: "lux", Domain: "web3.lux.network"},
		{Name: "bootnode", Domain: "bootno.de"},
	}

	ing := BuildCloudAPIIngress(c)

	if got := len(ing.Spec.TLS); got != 3 {
		t.Fatalf("want one TLS entry per brand (3), got %d", got)
	}
	for _, e := range ing.Spec.TLS {
		if len(e.Hosts) != 2 {
			t.Errorf("%s: want exactly api+ws (2 hosts), got %v", e.SecretName, e.Hosts)
		}
	}
	if got, want := ing.Spec.TLS[0].SecretName, "web3-platform-api-hanzo-tls"; got != want {
		t.Errorf("secret name = %q, want %q", got, want)
	}
	if got, want := ing.Spec.TLS[2].Hosts[0], "api.bootno.de"; got != want {
		t.Errorf("bootnode host = %q, want %q", got, want)
	}
	// routing must be untouched: api+ws per brand
	if got := len(ing.Spec.Rules); got != 6 {
		t.Errorf("rules changed: want 6, got %d", got)
	}
}

func TestBuildCloudAPIIngress_NoTLSWhenDisabled(t *testing.T) {
	c := &v1alpha1.Cloud{}
	c.Name = "web3-platform"
	c.Spec.Ingress = &v1alpha1.IngressSpec{Enabled: true, TLS: false}
	c.Spec.Brands = []v1alpha1.BrandConfig{{Name: "hanzo", Domain: "web3.hanzo.ai"}}
	if got := len(BuildCloudAPIIngress(c).Spec.TLS); got != 0 {
		t.Errorf("TLS disabled must emit no entries, got %d", got)
	}
}

// A host that cannot answer its ACME challenge must not block certificates for
// hosts on other domains, so BuildIngress groups TLS by registered domain.
func TestBuildIngress_TLSGroupedByDomain(t *testing.T) {
	spec := &v1alpha1.IngressSpec{
		Enabled: true,
		TLS:     true,
		Hosts:   []string{"api.web3.hanzo.ai", "ws.web3.hanzo.ai", "api.bootno.de", "ws.bootno.de"},
	}
	ing := BuildIngress("gw", "ns", spec, "gw", 80, nil)

	if got := len(ing.Spec.TLS); got != 2 {
		t.Fatalf("want 2 TLS entries (hanzo.ai + bootno.de), got %d", got)
	}
	if got, want := ing.Spec.TLS[0].SecretName, "gw-hanzo-ai-tls"; got != want {
		t.Errorf("secret = %q, want %q", got, want)
	}
	if got, want := len(ing.Spec.TLS[1].Hosts), 2; got != want {
		t.Errorf("bootno.de hosts = %d, want %d", got, want)
	}
	if got := len(ing.Spec.Rules); got != 4 {
		t.Errorf("routing changed: %d rules, want 4", got)
	}
}

// The overwhelmingly common case is one domain; that must keep its existing
// secret name so live Ingresses are not forced to re-issue.
func TestBuildIngress_SingleDomainKeepsSecretName(t *testing.T) {
	spec := &v1alpha1.IngressSpec{
		Enabled: true, TLS: true,
		Hosts: []string{"api.lux.network", "ws.lux.network"},
	}
	ing := BuildIngress("gw", "ns", spec, "gw", 80, nil)
	if len(ing.Spec.TLS) != 1 {
		t.Fatalf("want 1 entry, got %d", len(ing.Spec.TLS))
	}
	if got, want := ing.Spec.TLS[0].SecretName, "gw-tls"; got != want {
		t.Errorf("secret = %q, want unchanged %q", got, want)
	}
}
