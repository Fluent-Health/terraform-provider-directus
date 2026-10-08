package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories instantiate the provider for acceptance
// tests; the CLI reattaches to it for every terraform command.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"directus": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck requires the acceptance stack (docker-compose.test.yml).
func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, v := range []string{"DIRECTUS_URL", "DIRECTUS_TOKEN"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests (see CONTRIBUTING.md)", v)
		}
	}
}
