package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-directus/internal/directus"
)

var _ provider.Provider = &DirectusProvider{}

// DirectusProvider is the provider implementation.
type DirectusProvider struct {
	// version is the release version, "dev" for local builds and "test" in
	// acceptance tests.
	version string
}

// DirectusProviderModel is the provider configuration.
type DirectusProviderModel struct {
	URL   types.String `tfsdk:"url"`
	Token types.String `tfsdk:"token"`
}

// New returns a provider factory.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &DirectusProvider{version: version}
	}
}

func (p *DirectusProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "directus"
	resp.Version = p.version
}

func (p *DirectusProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage [Directus](https://directus.io) configuration (schema, access control, folders, flows, dashboards, settings) through its REST API.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Base URL of the Directus instance, e.g. `https://cms.example.com`. Can also be set with the `DIRECTUS_URL` environment variable.",
			},
			"token": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Static access token of a Directus user (set on the user's `token` field). Can also be set with the `DIRECTUS_TOKEN` environment variable.",
			},
		},
	}
}

func (p *DirectusProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data DirectusProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration wins over the environment.
	url := os.Getenv("DIRECTUS_URL")
	token := os.Getenv("DIRECTUS_TOKEN")
	if !data.URL.IsNull() {
		url = data.URL.ValueString()
	}
	if !data.Token.IsNull() {
		token = data.Token.ValueString()
	}

	if url == "" {
		resp.Diagnostics.AddAttributeError(path.Root("url"), "Missing Directus URL",
			"Set the url argument or the DIRECTUS_URL environment variable.")
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing Directus token",
			"Set the token argument or the DIRECTUS_TOKEN environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	client := directus.NewClient(url, token)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *DirectusProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewFolderResource,
	}
}

func (p *DirectusProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
