package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-directus/internal/directus"
)

var (
	_ resource.Resource                   = &FieldResource{}
	_ resource.ResourceWithImportState    = &FieldResource{}
	_ resource.ResourceWithModifyPlan     = &FieldResource{}
	_ resource.ResourceWithValidateConfig = &FieldResource{}
)

// fieldTypes are the Directus field types. "alias" covers every field with no
// column (o2m, m2m, m2a, files, translations, groups, presentation): Directus
// accepts those subtypes on create but always reads them back as "alias", so
// the subtype is expressed through `special` instead.
var fieldTypes = []string{
	"alias", "bigInteger", "boolean", "csv", "date", "dateTime", "decimal", "float",
	"hash", "integer", "json", "string", "text", "time", "timestamp", "uuid",
}

// requiredSpecial: types Directus derives from the column plus a special flag.
// Without the flag a csv column reads back as "text" and a hash as "string";
// json gets cast-json added by Directus. Requiring them keeps plans stable.
var requiredSpecial = map[string]string{
	"csv":  "cast-csv",
	"hash": "hash",
	"json": "cast-json",
}

// NewFieldResource returns the directus_field resource.
func NewFieldResource() resource.Resource {
	return &FieldResource{}
}

// FieldResource manages one field of a collection: its column (unless it is
// an alias) and its directus_fields row.
type FieldResource struct {
	client *directus.Client
}

// FieldResourceModel is the directus_field state.
type FieldResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Collection types.String `tfsdk:"collection"`
	Field      types.String `tfsdk:"field"`
	Type       types.String `tfsdk:"type"`

	// Column (null for alias fields).
	DefaultValue     jsontypes.Normalized `tfsdk:"default_value"`
	MaxLength        types.Int64          `tfsdk:"max_length"`
	NumericPrecision types.Int64          `tfsdk:"numeric_precision"`
	NumericScale     types.Int64          `tfsdk:"numeric_scale"`
	Nullable         types.Bool           `tfsdk:"nullable"`
	Unique           types.Bool           `tfsdk:"unique"`
	Indexed          types.Bool           `tfsdk:"indexed"`
	DataType         types.String         `tfsdk:"data_type"`
	ForeignKeyTable  types.String         `tfsdk:"foreign_key_table"`
	ForeignKeyColumn types.String         `tfsdk:"foreign_key_column"`

	// directus_fields row.
	Special           types.List           `tfsdk:"special"`
	Interface         types.String         `tfsdk:"interface"`
	Options           jsontypes.Normalized `tfsdk:"options"`
	Display           types.String         `tfsdk:"display"`
	DisplayOptions    jsontypes.Normalized `tfsdk:"display_options"`
	Readonly          types.Bool           `tfsdk:"readonly"`
	Hidden            types.Bool           `tfsdk:"hidden"`
	Sort              types.Int64          `tfsdk:"sort"`
	Width             types.String         `tfsdk:"width"`
	Translations      jsontypes.Normalized `tfsdk:"translations"`
	Note              types.String         `tfsdk:"note"`
	Conditions        jsontypes.Normalized `tfsdk:"conditions"`
	Required          types.Bool           `tfsdk:"required"`
	Group             types.String         `tfsdk:"group"`
	Validation        jsontypes.Normalized `tfsdk:"validation"`
	ValidationMessage types.String         `tfsdk:"validation_message"`
	Searchable        types.Bool           `tfsdk:"searchable"`

	AllowDestroy types.Bool `tfsdk:"allow_destroy"`
}

func (r *FieldResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_field"
}

func (r *FieldResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc}
	}
	json := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{CustomType: jsontypes.NormalizedType{}, Optional: true, MarkdownDescription: desc + " JSON (use `jsonencode`)."}
	}
	flag := func(def bool, desc string) schema.BoolAttribute {
		return schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(def), MarkdownDescription: desc}
	}
	computedInt := func(desc string) schema.Int64Attribute {
		return schema.Int64Attribute{Optional: true, Computed: true, MarkdownDescription: desc,
			PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}}
	}
	readOnly := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, MarkdownDescription: desc,
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: "A field of a collection: its database column (unless `type = \"alias\"`) and its Data Studio settings. " +
			"Destroying a field drops its column and every value in it, so it is guarded by `allow_destroy`. " +
			"Do not declare a collection's primary key here; `directus_collection` owns it. " +
			"See the [Directus fields API](https://directus.io/docs/api/fields).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "`<collection>.<field>`, the import ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"collection": schema.StringAttribute{
				Required: true, MarkdownDescription: "Collection the field belongs to. Changing it replaces the field.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"field": schema.StringAttribute{
				Required: true, MarkdownDescription: "Field (and column) name. Changing it replaces the field.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Directus type: one of `" + strings.Join(fieldTypes, "`, `") + "`. " +
					"`alias` is any field without a column (o2m, m2m, m2a, translations, groups, presentation); its kind goes in `special`. " +
					"Changing between two column types ALTERs the column in place; changing to or from `alias` replaces the field.",
				Validators: []validator.String{stringvalidator.OneOf(fieldTypes...)},
			},

			"default_value": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{}, Optional: true,
				MarkdownDescription: "Column default, as JSON so its type survives: `jsonencode(\"draft\")`, `jsonencode(0)`, `jsonencode(false)`, " +
					"`jsonencode(\"CURRENT_TIMESTAMP\")`. Write `CURRENT_TIMESTAMP` rather than `now()`, which Postgres reads back as `CURRENT_TIMESTAMP`.",
			},
			"max_length":         computedInt("Maximum length of a `string` column. Directus defaults it to 255."),
			"numeric_precision":  computedInt("Precision of a `decimal` column."),
			"numeric_scale":      computedInt("Scale of a `decimal` column."),
			"nullable":           flag(true, "Whether the column accepts NULL. Defaults to `true`."),
			"unique":             flag(false, "Add a unique index. Defaults to `false`."),
			"indexed":            flag(false, "Add a (non-unique) index. Defaults to `false`."),
			"data_type":          readOnly("Database column type, e.g. `character varying`."),
			"foreign_key_table":  readOnly("Table a foreign key on this column points at; set by a `directus_relation`."),
			"foreign_key_column": readOnly("Column a foreign key on this column points at; set by a `directus_relation`."),

			"special": schema.ListAttribute{
				ElementType: types.StringType, Optional: true,
				MarkdownDescription: "Special flags, e.g. `[\"uuid\"]`, `[\"m2o\"]`, `[\"date-created\"]`, `[\"cast-boolean\"]`. " +
					"Required: `cast-csv` for `csv`, `hash` for `hash`, `cast-json` for `json`, and for an `alias` one of `" + strings.Join(directus.AliasSpecials, "`, `") +
					"` (Directus does not list an alias field without one, e.g. a group needs `[\"alias\", \"no-data\", \"group\"]`).",
			},
			"interface":       str("Data Studio input interface, e.g. `input`, `select-dropdown-m2o`."),
			"options":         json("Interface options."),
			"display":         str("Display used to render the value, e.g. `related-values`."),
			"display_options": json("Display options."),
			"readonly":        flag(false, "Read-only in the Data Studio. Defaults to `false`."),
			"hidden":          flag(false, "Hidden in the Data Studio item form. Defaults to `false`."),
			"sort": schema.Int64Attribute{
				Optional: true, Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				MarkdownDescription: "Position in the item form. Directus appends a new field after the last one if unset.",
			},
			"width": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("full"),
				MarkdownDescription: "Form width: `half`, `half-left`, `half-right`, `full` (default) or `fill`.",
				Validators:          []validator.String{stringvalidator.OneOf("half", "half-left", "half-right", "full", "fill")},
			},
			"translations":       json("Field-name translations, e.g. `[{ language = \"en-US\", translation = \"Title\" }]`."),
			"note":               str("Help text shown under the field."),
			"conditions":         json("Conditional overrides of the field's settings."),
			"required":           flag(false, "Required in the Data Studio. Defaults to `false`."),
			"group":              str("Name of the group (an alias field with `group` special) that contains this field."),
			"validation":         json("Validation filter."),
			"validation_message": str("Message shown when `validation` fails."),
			"searchable":         flag(true, "Included in Data Studio search. Defaults to `true`."),

			"allow_destroy": allowDestroyAttribute("field (for a column: the column and every value in it)"),
		},
	}
}

func (r *FieldResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data FieldResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.Type.IsUnknown() || data.Special.IsUnknown() {
		return
	}
	typ := data.Type.ValueString()
	var special []string
	if !data.Special.IsNull() {
		resp.Diagnostics.Append(data.Special.ElementsAs(ctx, &special, false)...)
	}

	if want, ok := requiredSpecial[typ]; ok && !slices.Contains(special, want) {
		resp.Diagnostics.AddAttributeError(path.Root("special"), "Missing special flag",
			fmt.Sprintf("A %s field needs %q in special; Directus reads it back as a different type (or adds the flag itself) otherwise.", typ, want))
	}
	if typ == "alias" {
		if !slices.ContainsFunc(special, func(s string) bool { return slices.Contains(directus.AliasSpecials, s) }) {
			resp.Diagnostics.AddAttributeError(path.Root("special"), "Alias field is not listable",
				fmt.Sprintf("An alias field needs one of %v in special. Directus omits it from GET /fields otherwise, so it would look deleted on every plan.", directus.AliasSpecials))
		}
		for _, a := range []struct {
			name string
			set  bool
		}{
			{"default_value", !data.DefaultValue.IsNull()}, {"max_length", !data.MaxLength.IsNull()},
			{"numeric_precision", !data.NumericPrecision.IsNull()}, {"numeric_scale", !data.NumericScale.IsNull()},
		} {
			if a.set {
				resp.Diagnostics.AddAttributeError(path.Root(a.name), "Column setting on an alias field", "An alias field has no column.")
			}
		}
	}
	if typ != "decimal" && (!data.NumericPrecision.IsNull() || !data.NumericScale.IsNull()) {
		resp.Diagnostics.AddAttributeError(path.Root("numeric_precision"), "Precision only applies to decimal",
			"Directus derives precision for other numeric types (a float reads back as 24 whatever is sent), so setting it would never converge.")
	}
	if !data.DefaultValue.IsNull() && !data.DefaultValue.IsUnknown() && strings.EqualFold(strings.Trim(data.DefaultValue.ValueString(), `"`), "now()") {
		resp.Diagnostics.AddAttributeError(path.Root("default_value"), "Use CURRENT_TIMESTAMP",
			`Postgres stores now() as CURRENT_TIMESTAMP and Directus reads that back, so write jsonencode("CURRENT_TIMESTAMP").`)
	}
}

func (r *FieldResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

func (r *FieldResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	var state, plan FieldResourceModel
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if !req.Plan.Raw.IsNull() {
		resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if !req.Plan.Raw.IsNull() {
		// An alias has no column: the column defaults (nullable = true, …)
		// must not be planned for it, or every apply would be inconsistent.
		if plan.Type.ValueString() == "alias" {
			for _, a := range []string{"nullable", "unique", "indexed"} {
				resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(a), types.BoolNull())...)
			}
			for _, a := range []string{"max_length", "numeric_precision", "numeric_scale"} {
				resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(a), types.Int64Null())...)
			}
			for _, a := range []string{"data_type", "foreign_key_table", "foreign_key_column"} {
				resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(a), types.StringNull())...)
			}
		}
		// An in-place type change ALTERs the column, so its derived values are
		// no longer the state's (string -> text drops max_length, changes
		// data_type). Unknown, unless the config pins them.
		if !req.State.Raw.IsNull() && !plan.Type.Equal(state.Type) && plan.Type.ValueString() != "alias" {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("data_type"), types.StringUnknown())...)
			for _, a := range []string{"max_length", "numeric_precision", "numeric_scale"} {
				var cfg types.Int64
				resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(a), &cfg)...)
				if cfg.IsNull() {
					resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(a), types.Int64Unknown())...)
				}
			}
		}
		// Crossing the alias/column line cannot be done in place: Directus
		// rejects "Alias type cannot be changed".
		if !req.State.Raw.IsNull() && (plan.Type.ValueString() == "alias") != (state.Type.ValueString() == "alias") {
			resp.RequiresReplace = append(resp.RequiresReplace, path.Root("type"))
		}
	}

	var replacing []string
	if !req.State.Raw.IsNull() && !req.Plan.Raw.IsNull() {
		if !plan.Collection.Equal(state.Collection) {
			replacing = append(replacing, "collection")
		}
		if !plan.Field.Equal(state.Field) {
			replacing = append(replacing, "field")
		}
		if (plan.Type.ValueString() == "alias") != (state.Type.ValueString() == "alias") {
			replacing = append(replacing, "type (to or from alias)")
		}
	}
	consequence := "drops the column and every value in it"
	if state.Type.ValueString() == "alias" {
		consequence = "removes the field from the Data Studio (an o2m/m2m alias also loses its relation's link back)"
	}
	guardDestroy(ctx, req, resp, "directus_field", state.Collection.ValueString()+"."+state.Field.ValueString(), consequence, replacing)
}

func (r *FieldResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data FieldResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &directus.FieldWrite{Field: data.Field.ValueString(), Type: data.Type.ValueString()}
	var diags diag.Diagnostics
	body.Meta, diags = fieldMetaFromModel(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if data.Type.ValueString() != "alias" {
		body.Schema = fieldSchemaFromModel(&data)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.CreateField(ctx, data.Collection.ValueString(), body); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create field %s.%s: %s", data.Collection.ValueString(), data.Field.ValueString(), err))
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FieldResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data FieldResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.GetField(ctx, data.Collection.ValueString(), data.Field.ValueString()); directus.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FieldResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, state FieldResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := &directus.FieldWrite{Type: data.Type.ValueString()}
	var diags diag.Diagnostics
	body.Meta, diags = fieldMetaFromModel(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Send schema only when the column changes: Directus ALTERs the column for
	// ANY schema object, which on a large table is a lock per metadata edit.
	// A type change needs schema to take effect at all.
	if data.Type.ValueString() != "alias" && columnChanged(&data, &state) {
		body.Schema = fieldSchemaFromModel(&data)
	}

	if err := r.client.UpdateField(ctx, data.Collection.ValueString(), data.Field.ValueString(), body); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update field %s.%s: %s", data.Collection.ValueString(), data.Field.ValueString(), err))
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *FieldResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data FieldResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := data.Collection.ValueString() + "." + data.Field.ValueString()
	if !data.AllowDestroy.ValueBool() {
		resp.Diagnostics.AddError("Refusing to destroy directus_field "+id, "allow_destroy is false.")
		return
	}
	if err := r.client.DeleteField(ctx, data.Collection.ValueString(), data.Field.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete field %s: %s", id, err))
	}
}

// ImportState takes "<collection>.<field>".
func (r *FieldResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	collection, field, ok := strings.Cut(req.ID, ".")
	if !ok || collection == "" || field == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected <collection>.<field>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("collection"), collection)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("field"), field)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_destroy"), false)...)
}

// columnChanged reports whether any column attribute differs between plan and
// state. Unknown planned values (UseStateForUnknown) count as unchanged.
func columnChanged(plan, state *FieldResourceModel) bool {
	changedInt := func(p, s types.Int64) bool { return !p.IsUnknown() && !p.Equal(s) }
	return !plan.Type.Equal(state.Type) ||
		!plan.DefaultValue.Equal(state.DefaultValue) ||
		changedInt(plan.MaxLength, state.MaxLength) ||
		changedInt(plan.NumericPrecision, state.NumericPrecision) ||
		changedInt(plan.NumericScale, state.NumericScale) ||
		!plan.Nullable.Equal(state.Nullable) ||
		!plan.Unique.Equal(state.Unique) ||
		!plan.Indexed.Equal(state.Indexed)
}

// fieldSchemaFromModel builds the column part of a write. Unknown/null
// computed sizes are left out so Directus applies its defaults.
func fieldSchemaFromModel(m *FieldResourceModel) map[string]any {
	s := map[string]any{
		"is_nullable":   m.Nullable.ValueBool(),
		"is_unique":     m.Unique.ValueBool(),
		"is_indexed":    m.Indexed.ValueBool(),
		"default_value": nil,
	}
	if !m.DefaultValue.IsNull() && !m.DefaultValue.IsUnknown() {
		s["default_value"] = rawFromJSON(m.DefaultValue)
	}
	for k, v := range map[string]types.Int64{"max_length": m.MaxLength, "numeric_precision": m.NumericPrecision, "numeric_scale": m.NumericScale} {
		if !v.IsNull() && !v.IsUnknown() {
			s[k] = v.ValueInt64()
		}
	}
	return s
}

// fieldMetaFromModel builds the full directus_fields row. Every managed key is
// sent, so removing a setting from config resets it.
func fieldMetaFromModel(ctx context.Context, m *FieldResourceModel) (map[string]any, diag.Diagnostics) {
	var diags diag.Diagnostics
	var special []string
	if !m.Special.IsNull() && !m.Special.IsUnknown() {
		diags.Append(m.Special.ElementsAs(ctx, &special, false)...)
	}
	meta := map[string]any{
		"special":            special,
		"interface":          m.Interface.ValueStringPointer(),
		"options":            rawFromJSON(m.Options),
		"display":            m.Display.ValueStringPointer(),
		"display_options":    rawFromJSON(m.DisplayOptions),
		"readonly":           m.Readonly.ValueBool(),
		"hidden":             m.Hidden.ValueBool(),
		"width":              m.Width.ValueString(),
		"translations":       rawFromJSON(m.Translations),
		"note":               m.Note.ValueStringPointer(),
		"conditions":         rawFromJSON(m.Conditions),
		"required":           m.Required.ValueBool(),
		"group":              m.Group.ValueStringPointer(),
		"validation":         rawFromJSON(m.Validation),
		"validation_message": m.ValidationMessage.ValueStringPointer(),
		"searchable":         m.Searchable.ValueBool(),
	}
	if !m.Sort.IsNull() && !m.Sort.IsUnknown() {
		meta["sort"] = m.Sort.ValueInt64()
	}
	return meta, diags
}

// read refreshes everything but collection, field and allow_destroy.
func (r *FieldResource) read(ctx context.Context, data *FieldResourceModel) (diags diag.Diagnostics) {
	f, err := r.client.GetField(ctx, data.Collection.ValueString(), data.Field.ValueString())
	if err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to read field %s.%s: %s", data.Collection.ValueString(), data.Field.ValueString(), err))
		return diags
	}

	data.ID = types.StringValue(f.Collection + "." + f.Field)
	data.Type = types.StringValue(f.Type)
	if s := f.Schema; s != nil {
		data.DefaultValue = jsonFromRaw(s.DefaultValue)
		data.MaxLength = types.Int64PointerValue(s.MaxLength)
		data.NumericPrecision = types.Int64PointerValue(s.NumericPrecision)
		data.NumericScale = types.Int64PointerValue(s.NumericScale)
		data.Nullable = types.BoolValue(s.IsNullable)
		data.Unique = types.BoolValue(s.IsUnique)
		data.Indexed = types.BoolValue(s.IsIndexed)
		data.DataType = types.StringValue(s.DataType)
		data.ForeignKeyTable = types.StringPointerValue(s.ForeignKeyTable)
		data.ForeignKeyColumn = types.StringPointerValue(s.ForeignKeyColumn)
	} else {
		data.DefaultValue = jsontypes.NewNormalizedNull()
		data.MaxLength, data.NumericPrecision, data.NumericScale = types.Int64Null(), types.Int64Null(), types.Int64Null()
		data.Nullable, data.Unique, data.Indexed = types.BoolNull(), types.BoolNull(), types.BoolNull()
		data.DataType, data.ForeignKeyTable, data.ForeignKeyColumn = types.StringNull(), types.StringNull(), types.StringNull()
	}

	m, err := f.ParsedMeta()
	if err != nil {
		diags.AddError("Client Error", err.Error())
		return diags
	}
	if m == nil {
		// No directus_fields row: Directus shows the field with defaults.
		full := "full"
		m = &FieldMeta{Width: &full, Searchable: true}
	}
	data.Special = types.ListNull(types.StringType)
	if len(m.Special) > 0 {
		l, d := types.ListValueFrom(ctx, types.StringType, m.Special)
		diags.Append(d...)
		data.Special = l
	}
	data.Interface = types.StringPointerValue(m.Interface)
	data.Options = jsonFromRaw(m.Options)
	data.Display = types.StringPointerValue(m.Display)
	data.DisplayOptions = jsonFromRaw(m.DisplayOptions)
	data.Readonly = types.BoolValue(m.Readonly)
	data.Hidden = types.BoolValue(m.Hidden)
	data.Sort = types.Int64PointerValue(m.Sort)
	data.Width = types.StringPointerValue(m.Width)
	data.Translations = jsonFromRaw(m.Translations)
	data.Note = types.StringPointerValue(m.Note)
	data.Conditions = jsonFromRaw(m.Conditions)
	data.Required = types.BoolValue(m.Required)
	data.Group = types.StringPointerValue(m.Group)
	data.Validation = jsonFromRaw(m.Validation)
	data.ValidationMessage = types.StringPointerValue(m.ValidationMessage)
	data.Searchable = types.BoolValue(m.Searchable)
	return diags
}

// FieldMeta aliases the client type for the defaults above.
type FieldMeta = directus.FieldMeta
