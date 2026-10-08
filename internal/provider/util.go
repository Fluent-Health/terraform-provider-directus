package provider

import (
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/Fluent-Health/terraform-provider-directus/internal/directus"
)

// configureClient extracts the *directus.Client from ProviderData. It returns
// nil before the provider is configured; the caller just returns then.
func configureClient(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *directus.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*directus.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *directus.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return nil
	}
	return c
}

// uuidRegexp matches a lower-case UUID. Postgres returns uuid columns in lower
// case, so an upper-case value in config would diff on every plan.
var uuidRegexp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
