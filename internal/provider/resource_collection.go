package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/Fluent-Health/terraform-provider-directus/internal/directus"
)

var (
	_ resource.Resource                   = &CollectionResource{}
	_ resource.ResourceWithImportState    = &CollectionResource{}
	_ resource.ResourceWithModifyPlan     = &CollectionResource{}
	_ resource.ResourceWithValidateConfig = &CollectionResource{}
)

// Primary-key types the provider can create, and the field each one becomes.
// They match what the Data Studio's "create collection" dialog creates.
var primaryKeyFields = map[string]func(name string) directus.FieldCreate{
	"integer": func(name string) directus.FieldCreate {
		return directus.FieldCreate{Field: name, Type: "integer",
			Meta:   map[string]any{"hidden": true, "readonly": true, "interface": "numeric"},
			Schema: map[string]any{"is_primary_key": true, "has_auto_increment": true}}
	},
	"bigInteger": func(name string) directus.FieldCreate {
		return directus.FieldCreate{Field: name, Type: "bigInteger",
			Meta:   map[string]any{"hidden": true, "readonly": true, "interface": "numeric"},
			Schema: map[string]any{"is_primary_key": true, "has_auto_increment": true}}
	},
	"uuid": func(name string) directus.FieldCreate {
		return directus.FieldCreate{Field: name, Type: "uuid",
			Meta:   map[string]any{"hidden": true, "readonly": true, "interface": "input", "special": []string{"uuid"}},
			Schema: map[string]any{"is_primary_key": true, "length": 36, "has_auto_increment": false}}
	},
	"string": func(name string) directus.FieldCreate {
		return directus.FieldCreate{Field: name, Type: "string",
			Meta:   map[string]any{"hidden": false, "readonly": false, "interface": "input"},
			Schema: map[string]any{"is_primary_key": true, "length": 255, "has_auto_increment": false}}
	},
}

// collectionNameRegexp mirrors Directus' own rules: no surrounding whitespace,
// no "/", and not the reserved directus_ prefix.
var collectionNameRegexp = regexp.MustCompile(`^[^\s/][^/]*[^\s/]$|^[^\s/]$`)

// NewCollectionResource returns the directus_collection resource.
func NewCollectionResource() resource.Resource {
	return &CollectionResource{}
}

// CollectionResource manages a collection: a table, or a group (a folder in
// the data model with no table).
type CollectionResource struct {
	client *directus.Client
}

// CollectionResourceModel is the directus_collection state.
type CollectionResourceModel struct {
	Collection               types.String         `tfsdk:"collection"`
	Table                    types.Bool           `tfsdk:"table"`
	PrimaryKey               types.Object         `tfsdk:"primary_key"`
	Icon                     types.String         `tfsdk:"icon"`
	Note                     types.String         `tfsdk:"note"`
	DisplayTemplate          types.String         `tfsdk:"display_template"`
	Hidden                   types.Bool           `tfsdk:"hidden"`
	Singleton                types.Bool           `tfsdk:"singleton"`
	Translations             jsontypes.Normalized `tfsdk:"translations"`
	ArchiveField             types.String         `tfsdk:"archive_field"`
	ArchiveAppFilter         types.Bool           `tfsdk:"archive_app_filter"`
	ArchiveValue             types.String         `tfsdk:"archive_value"`
	UnarchiveValue           types.String         `tfsdk:"unarchive_value"`
	SortField                types.String         `tfsdk:"sort_field"`
	Accountability           types.String         `tfsdk:"accountability"`
	Color                    types.String         `tfsdk:"color"`
	ItemDuplicationFields    types.List           `tfsdk:"item_duplication_fields"`
	Sort                     types.Int64          `tfsdk:"sort"`
	Group                    types.String         `tfsdk:"group"`
	Collapse                 types.String         `tfsdk:"collapse"`
	PreviewURL               types.String         `tfsdk:"preview_url"`
	Versioning               types.Bool           `tfsdk:"versioning"`
	AutosaveRevisionInterval types.Int64          `tfsdk:"autosave_revision_interval"`
	AllowDestroy             types.Bool           `tfsdk:"allow_destroy"`
}

var primaryKeyAttrTypes = map[string]attr.Type{
	"field": types.StringType,
	"type":  types.StringType,
}

func (r *CollectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_collection"
}

func (r *CollectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	nullableString := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A collection: a database table, or (with `table = false`) a group that only organises other collections in the data model. " +
			"Destroying a table collection drops the table and every row in it, so it is guarded by `allow_destroy`. " +
			"See the [Directus collections API](https://directus.io/docs/api/collections).",
		Attributes: map[string]schema.Attribute{
			"collection": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Collection name: the table name for a table collection. Changing it replaces the collection.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(collectionNameRegexp, "must not contain \"/\" or start or end with whitespace"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"table": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "`true` (default) for a table collection; `false` for a group with no table. Directus cannot convert one into the other, so changing it replaces the collection.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"primary_key": schema.SingleNestedAttribute{
				Optional: true,
				Computed: true,
				MarkdownDescription: "The primary-key field, created with the table and owned by this resource (do not also declare it as a `directus_field`). " +
					"Omit it for an auto-increment integer `id`. Not allowed on a group. Changing it replaces the collection.",
				Attributes: map[string]schema.Attribute{
					"field": schema.StringAttribute{
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString("id"),
						MarkdownDescription: "Field name. Defaults to `id`.",
					},
					"type": schema.StringAttribute{
						Required:            true,
						MarkdownDescription: "`integer` or `bigInteger` (auto-increment), `uuid` (generated by Directus), or `string` (set by the client).",
						Validators:          []validator.String{stringvalidator.OneOf("integer", "bigInteger", "uuid", "string")},
					},
				},
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
					objectplanmodifier.RequiresReplace(),
				},
			},
			"icon":             nullableString("Material icon name shown for the collection."),
			"note":             nullableString("Help text shown under the collection name."),
			"display_template": nullableString("Template used to display an item of this collection, e.g. `{{title}}`."),
			"hidden": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Hide the collection from the Data Studio navigation. Defaults to `false`.",
			},
			"singleton": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Treat the collection as a single item. Defaults to `false`.",
			},
			"translations": schema.StringAttribute{
				CustomType:          jsontypes.NormalizedType{},
				Optional:            true,
				MarkdownDescription: "Collection-name translations, as JSON: `jsonencode([{ language = \"en-US\", translation = \"Articles\", singular = \"Article\", plural = \"Articles\" }])`.",
			},
			"archive_field": nullableString("Field that holds the archive status."),
			"archive_app_filter": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Hide archived items in the Data Studio by default. Defaults to `true`.",
			},
			"archive_value":   nullableString("Value of `archive_field` that marks an item archived."),
			"unarchive_value": nullableString("Value `archive_field` is set to when an item is unarchived."),
			"sort_field":      nullableString("Field used for manual drag-and-drop sorting."),
			"accountability": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("all"),
				MarkdownDescription: "What Directus tracks for this collection: `all` (activity and revisions, the default) or `activity`.",
				Validators:          []validator.String{stringvalidator.OneOf("all", "activity")},
			},
			"color": nullableString("Hex colour of the collection's icon."),
			"item_duplication_fields": schema.ListAttribute{
				ElementType:         types.StringType,
				Optional:            true,
				MarkdownDescription: "Fields copied when an item is duplicated in the Data Studio.",
			},
			"sort": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Position of the collection in the navigation, within its group.",
			},
			"group": nullableString("Name of the parent group collection."),
			"collapse": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("open"),
				MarkdownDescription: "For a group: `open` (default), `closed`, or `locked`.",
				Validators:          []validator.String{stringvalidator.OneOf("open", "closed", "locked")},
			},
			"preview_url": nullableString("URL template for the live-preview pane."),
			"versioning": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Enable content versioning. Defaults to `false`.",
			},
			"autosave_revision_interval": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Autosave interval for revisions, in seconds.",
			},
			"allow_destroy": allowDestroyAttribute("collection (for a table: its table and every row)"),
		},
	}
}

func (r *CollectionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data CollectionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if name := data.Collection.ValueString(); strings.HasPrefix(name, "directus_") {
		resp.Diagnostics.AddAttributeError(path.Root("collection"), "Reserved collection name",
			fmt.Sprintf("%q starts with directus_, which Directus reserves for its own collections.", name))
	}
	if !data.Table.IsNull() && !data.Table.IsUnknown() && !data.Table.ValueBool() &&
		!data.PrimaryKey.IsNull() && !data.PrimaryKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("primary_key"), "primary_key on a group",
			"A group (table = false) has no table, so it has no primary key.")
	}
}

func (r *CollectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *CollectionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var state, plan CollectionResourceModel
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if !req.Plan.Raw.IsNull() {
		resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Mirrors the RequiresReplace plan modifiers on these attributes.
	var replacing []string
	if !req.State.Raw.IsNull() && !req.Plan.Raw.IsNull() {
		if !plan.Collection.Equal(state.Collection) {
			replacing = append(replacing, "collection")
		}
		if !plan.Table.Equal(state.Table) {
			replacing = append(replacing, "table")
		}
		if !plan.PrimaryKey.IsUnknown() && !plan.PrimaryKey.Equal(state.PrimaryKey) {
			replacing = append(replacing, "primary_key")
		}
	}
	consequence := "drops the table and every row in it"
	if !state.Table.ValueBool() {
		consequence = "removes the group (child collections move to the top level)"
	}
	guardDestroy(ctx, req, resp, "directus_collection", state.Collection.ValueString(), consequence, replacing)
}

func (r *CollectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data CollectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	meta, diags := collectionMetaFromModel(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := &directus.CollectionCreate{Collection: data.Collection.ValueString(), Meta: meta}
	if data.Table.ValueBool() {
		body.Schema = &struct{}{}
		pkName, pkType := "id", "integer"
		if !data.PrimaryKey.IsNull() && !data.PrimaryKey.IsUnknown() {
			attrs := data.PrimaryKey.Attributes()
			if v, ok := attrs["field"].(basetypes.StringValue); ok && !v.IsNull() && !v.IsUnknown() {
				pkName = v.ValueString()
			}
			if v, ok := attrs["type"].(basetypes.StringValue); ok && !v.IsNull() && !v.IsUnknown() {
				pkType = v.ValueString()
			}
		}
		body.Fields = []directus.FieldCreate{primaryKeyFields[pkType](pkName)}
	}

	if _, err := r.client.CreateCollection(ctx, body); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create collection %s: %s", body.Collection, err))
		return
	}

	resp.Diagnostics.Append(r.read(ctx, &data)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data CollectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.client.GetCollection(ctx, data.Collection.ValueString())
	if directus.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data CollectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	meta, diags := collectionMetaFromModel(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.UpdateCollectionMeta(ctx, data.Collection.ValueString(), meta); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update collection %s: %s", data.Collection.ValueString(), err))
		return
	}

	resp.Diagnostics.Append(r.read(ctx, &data)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *CollectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data CollectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// ModifyPlan already refused this plan; this is the backstop for a plan
	// made by an older provider version.
	if !data.AllowDestroy.ValueBool() {
		resp.Diagnostics.AddError("Refusing to destroy directus_collection "+data.Collection.ValueString(), "allow_destroy is false.")
		return
	}
	if err := r.client.DeleteCollection(ctx, data.Collection.ValueString()); err != nil && !directus.IsNotFound(err) {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete collection %s: %s", data.Collection.ValueString(), err))
	}
}

func (r *CollectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("collection"), req, resp)
	// An imported object is guarded until the config opts in.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_destroy"), false)...)
}

// read refreshes everything but collection and allow_destroy from Directus.
func (r *CollectionResource) read(ctx context.Context, data *CollectionResourceModel) (diags diag.Diagnostics) {
	c, err := r.client.GetCollection(ctx, data.Collection.ValueString())
	if err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to read collection %s: %s", data.Collection.ValueString(), err))
		return diags
	}

	data.Table = types.BoolValue(c.Schema != nil)
	data.PrimaryKey = types.ObjectNull(primaryKeyAttrTypes)
	if c.Schema != nil {
		pk, err := r.client.PrimaryKeyField(ctx, c.Collection)
		if err != nil {
			diags.AddError("Client Error", fmt.Sprintf("Unable to read the primary key of collection %s: %s", c.Collection, err))
			return diags
		}
		obj, d := types.ObjectValue(primaryKeyAttrTypes, map[string]attr.Value{
			"field": types.StringValue(pk.Field),
			"type":  types.StringValue(pk.Type),
		})
		diags.Append(d...)
		data.PrimaryKey = obj
	}

	// A table created outside Directus' API has no meta row; Directus then
	// shows every setting at its default, which is what the model gets.
	m := c.Meta
	if m == nil {
		m = &directus.CollectionMeta{ArchiveAppFilter: true, Collapse: "open"}
		all := "all"
		m.Accountability = &all
	}
	data.Icon = types.StringPointerValue(m.Icon)
	data.Note = types.StringPointerValue(m.Note)
	data.DisplayTemplate = types.StringPointerValue(m.DisplayTemplate)
	data.Hidden = types.BoolValue(m.Hidden)
	data.Singleton = types.BoolValue(m.Singleton)
	data.Translations = jsonFromRaw(m.Translations)
	data.ArchiveField = types.StringPointerValue(m.ArchiveField)
	data.ArchiveAppFilter = types.BoolValue(m.ArchiveAppFilter)
	data.ArchiveValue = types.StringPointerValue(m.ArchiveValue)
	data.UnarchiveValue = types.StringPointerValue(m.UnarchiveValue)
	data.SortField = types.StringPointerValue(m.SortField)
	data.Accountability = types.StringPointerValue(m.Accountability)
	data.Color = types.StringPointerValue(m.Color)
	data.ItemDuplicationFields = types.ListNull(types.StringType)
	if m.ItemDuplicationFields != nil {
		l, d := types.ListValueFrom(ctx, types.StringType, m.ItemDuplicationFields)
		diags.Append(d...)
		data.ItemDuplicationFields = l
	}
	data.Sort = types.Int64PointerValue(m.Sort)
	data.Group = types.StringPointerValue(m.Group)
	data.Collapse = types.StringValue(m.Collapse)
	data.PreviewURL = types.StringPointerValue(m.PreviewURL)
	data.Versioning = types.BoolValue(m.Versioning)
	data.AutosaveRevisionInterval = types.Int64PointerValue(m.AutosaveRevisionInterval)
	return diags
}

func collectionMetaFromModel(ctx context.Context, m *CollectionResourceModel) (*directus.CollectionMeta, diag.Diagnostics) {
	var diags diag.Diagnostics
	meta := &directus.CollectionMeta{
		Icon:                     m.Icon.ValueStringPointer(),
		Note:                     m.Note.ValueStringPointer(),
		DisplayTemplate:          m.DisplayTemplate.ValueStringPointer(),
		Hidden:                   m.Hidden.ValueBool(),
		Singleton:                m.Singleton.ValueBool(),
		Translations:             rawFromJSON(m.Translations),
		ArchiveField:             m.ArchiveField.ValueStringPointer(),
		ArchiveAppFilter:         m.ArchiveAppFilter.ValueBool(),
		ArchiveValue:             m.ArchiveValue.ValueStringPointer(),
		UnarchiveValue:           m.UnarchiveValue.ValueStringPointer(),
		SortField:                m.SortField.ValueStringPointer(),
		Accountability:           m.Accountability.ValueStringPointer(),
		Color:                    m.Color.ValueStringPointer(),
		Sort:                     m.Sort.ValueInt64Pointer(),
		Group:                    m.Group.ValueStringPointer(),
		Collapse:                 m.Collapse.ValueString(),
		PreviewURL:               m.PreviewURL.ValueStringPointer(),
		Versioning:               m.Versioning.ValueBool(),
		AutosaveRevisionInterval: m.AutosaveRevisionInterval.ValueInt64Pointer(),
	}
	if !m.ItemDuplicationFields.IsNull() {
		diags.Append(m.ItemDuplicationFields.ElementsAs(ctx, &meta.ItemDuplicationFields, false)...)
	}
	return meta, diags
}

// jsonFromRaw maps a nullable JSON column to a Normalized value; JSON null and
// an absent value are both Terraform null.
func jsonFromRaw(raw json.RawMessage) jsontypes.Normalized {
	if len(raw) == 0 || string(raw) == "null" {
		return jsontypes.NewNormalizedNull()
	}
	return jsontypes.NewNormalizedValue(string(raw))
}

// rawFromJSON is the inverse of jsonFromRaw: null becomes JSON null.
func rawFromJSON(v jsontypes.Normalized) json.RawMessage {
	if v.IsNull() || v.IsUnknown() {
		return json.RawMessage("null")
	}
	return json.RawMessage(v.ValueString())
}
