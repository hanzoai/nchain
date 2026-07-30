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
