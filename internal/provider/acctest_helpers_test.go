package provider

import (
	"os"
	"os/exec"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories wires the in-process provider for the
// terraform-plugin-testing harness.
func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"tcpshield": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// accEnv prepares the acceptance environment shared by the mock and the live
// tests.
func accEnv(t *testing.T) {
	t.Helper()
	// The harness reattaches the provider under the address tofu resolves a bare
	// "tcpshield" to (registry.opentofu.org/hashicorp/...); the framework
	// otherwise defaults to registry.terraform.io/-/..., which tofu rejects.
	t.Setenv("TF_ACC_PROVIDER_HOST", "registry.opentofu.org")
	t.Setenv("TF_ACC_PROVIDER_NAMESPACE", "hashicorp")

	// Let `TF_ACC=1 go test` find a TF binary without the caller exporting a
	// path: prefer an explicit one, else fall back to tofu on PATH.
	if os.Getenv("TF_ACC_TERRAFORM_PATH") == "" {
		if tofu, err := exec.LookPath("tofu"); err == nil {
			t.Setenv("TF_ACC_TERRAFORM_PATH", tofu)
		}
	}
}
