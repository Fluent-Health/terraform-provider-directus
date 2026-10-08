package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-directus/internal/directus"
)

var (
	_ resource.Resource                = &FolderResource{}
	_ resource.ResourceWithImportState = &FolderResource{}
)

// NewFolderResource returns the directus_folder resource.
func NewFolderResource() resource.Resource {
	return &FolderResource{}
}

// FolderResource manages a row of directus_folders.
type FolderResource struct {
	client *directus.Client
}

// FolderResourceModel is the directus_folder state.
type FolderResourceModel struct {
	ID     types.String `tfsdk:"id"`
	Name   types.String `tfsdk:"name"`
	Parent types.String `tfsdk:"parent"`
	Type   types.String `tfsdk:"type"`
}

func (r *FolderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_folder"
}

func (r *FolderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A folder (`directus_folders`): a file-library folder, or a Flows-module folder with `type = \"flows\"`. See the [Directus folders API](https://directus.io/docs/api/folders).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Folder UUID. Set it to give the folder the same id in every environment; omit it and Directus generates one. Changing it replaces the folder.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegexp, "must be a lower-case UUID"),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Folder name.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"parent": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "UUID of the parent folder. Omit for a root folder.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(uuidRegexp, "must be a lower-case UUID"),
				},
			},
			"type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(directus.FolderTypeFiles),
				MarkdownDescription: "`files` (file library, the default) or `flows` (groups flows in the Flows module). Directus hides `flows` folders from non-admin users, so managing one needs an admin token.",
				Validators: []validator.String{
					stringvalidator.OneOf(directus.FolderTypeFiles, directus.FolderTypeFlows),
				},
			},
		},
	}
}

func (r *FolderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *FolderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data FolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	f, err := r.client.CreateFolder(ctx, folderFromModel(&data))
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create folder: %s", err))
		return
	}

	folderToModel(f, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FolderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data FolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	f, err := r.client.GetFolder(ctx, data.ID.ValueString())
	if directus.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read folder %s: %s", data.ID.ValueString(), err))
		return
	}

	folderToModel(f, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FolderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data FolderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	f, err := r.client.UpdateFolder(ctx, folderFromModel(&data))
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update folder %s: %s", data.ID.ValueString(), err))
		return
	}

	folderToModel(f, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FolderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FolderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteFolder(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete folder %s: %s", data.ID.ValueString(), err))
	}
}

func (r *FolderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func folderFromModel(m *FolderResourceModel) *directus.Folder {
	return &directus.Folder{
		ID:     m.ID.ValueString(),
		Name:   m.Name.ValueString(),
		Parent: m.Parent.ValueStringPointer(),
		Type:   m.Type.ValueString(),
	}
}

func folderToModel(f *directus.Folder, m *FolderResourceModel) {
	m.ID = types.StringValue(f.ID)
	m.Name = types.StringValue(f.Name)
	m.Parent = types.StringPointerValue(f.Parent)
	m.Type = types.StringValue(f.Type)
}
