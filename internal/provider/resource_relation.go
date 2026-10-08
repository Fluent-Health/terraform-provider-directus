package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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
	_ resource.Resource                   = &RelationResource{}
	_ resource.ResourceWithImportState    = &RelationResource{}
	_ resource.ResourceWithModifyPlan     = &RelationResource{}
	_ resource.ResourceWithValidateConfig = &RelationResource{}
)

var fkActions = []string{"NO ACTION", "SET NULL", "SET DEFAULT", "CASCADE", "RESTRICT"}

// NewRelationResource returns the directus_relation resource.
func NewRelationResource() resource.Resource {
	return &RelationResource{}
}

// RelationResource manages a relation, keyed by its many side. One relation
// expresses an m2o (and, with one_field, the o2m alias pointing back at it);
// an m2m or translations is two relations on a junction collection; an m2a has
// no related_collection and no foreign key.
type RelationResource struct {
	client *directus.Client
}

// RelationResourceModel is the directus_relation state.
type RelationResourceModel struct {
	ID                    types.String `tfsdk:"id"`
	Collection            types.String `tfsdk:"collection"`
	Field                 types.String `tfsdk:"field"`
	RelatedCollection     types.String `tfsdk:"related_collection"`
	OnDelete              types.String `tfsdk:"on_delete"`
	OnUpdate              types.String `tfsdk:"on_update"`
	ConstraintName        types.String `tfsdk:"constraint_name"`
	OneField              types.String `tfsdk:"one_field"`
	OneCollectionField    types.String `tfsdk:"one_collection_field"`
	OneAllowedCollections types.List   `tfsdk:"one_allowed_collections"`
	JunctionField         types.String `tfsdk:"junction_field"`
	SortField             types.String `tfsdk:"sort_field"`
	OneDeselectAction     types.String `tfsdk:"one_deselect_action"`
}

func (r *RelationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_relation"
}

func (r *RelationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	str := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Optional: true, MarkdownDescription: desc}
	}
	fkAction := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{
			Optional: true, Computed: true, MarkdownDescription: desc + " One of `" + strings.Join(fkActions, "`, `") + "`. Defaults to `NO ACTION`; not set for an m2a.",
			Validators: []validator.String{stringvalidator.OneOf(fkActions...)},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A relation, keyed by its many side (`collection`.`field`). " +
			"An m2o is one relation (plus `one_field` for the o2m alias pointing back); an m2m or translations field is two relations on a junction collection; " +
			"an m2a has no `related_collection` and lists `one_allowed_collections` instead. " +
			"The many-side field must exist first (reference its `directus_field`). Deleting a relation drops only its foreign key; the field and its data stay. " +
			"See the [Directus relations API](https://directus.io/docs/api/relations).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, MarkdownDescription: "`<collection>.<field>` of the many side, the import ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"collection": schema.StringAttribute{
				Required: true, MarkdownDescription: "Collection holding the foreign key (the many side). Changing it replaces the relation.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"field": schema.StringAttribute{
				Required: true, MarkdownDescription: "Foreign-key field on `collection`. Changing it replaces the relation.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"related_collection": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Collection the foreign key points at (the one side). Omit for an m2a. " +
					"Directus cannot retarget a relation (it keeps the old one and answers 200), so changing it replaces the relation.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"on_delete": fkAction("Foreign-key action when the related item is deleted."),
			"on_update": fkAction("Foreign-key action when the related item's key changes."),
			"constraint_name": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Name of the foreign-key constraint.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"one_field":            str("O2M alias field on `related_collection` that lists the items pointing at it."),
			"one_collection_field": str("For an m2a: field on `collection` that stores which collection the item is from."),
			"one_allowed_collections": schema.ListAttribute{
				ElementType: types.StringType, Optional: true,
				MarkdownDescription: "For an m2a: collections the item may come from.",
			},
			"junction_field": str("For an m2m/m2a junction: the other foreign-key field on the junction."),
			"sort_field":     str("Field on `collection` used to sort the related items."),
			"one_deselect_action": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("nullify"),
				MarkdownDescription: "What deselecting an item in the o2m interface does: `nullify` (default) or `delete`.",
				Validators:          []validator.String{stringvalidator.OneOf("nullify", "delete")},
			},
		},
	}
}

func (r *RelationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data RelationResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || data.RelatedCollection.IsUnknown() {
		return
	}
	if data.RelatedCollection.IsNull() {
		for _, a := range []struct {
			name string
			set  bool
		}{{"on_delete", !data.OnDelete.IsNull()}, {"on_update", !data.OnUpdate.IsNull()}} {
			if a.set {
				resp.Diagnostics.AddAttributeError(path.Root(a.name), "No foreign key on an m2a",
					"Without related_collection the relation is an m2a, which has no foreign key to act on.")
			}
		}
	}
}

func (r *RelationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.client = configureClient(req, resp)
}

// ModifyPlan plans the foreign-key actions: NO ACTION when unset on a relation
// with a foreign key, null on an m2a.
func (r *RelationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan RelationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.RelatedCollection.IsUnknown() {
		return
	}
	m2a := plan.RelatedCollection.IsNull()
	for _, a := range []struct {
		name string
		v    types.String
	}{{"on_delete", plan.OnDelete}, {"on_update", plan.OnUpdate}} {
		switch {
		case m2a:
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(a.name), types.StringNull())...)
		case a.v.IsUnknown():
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root(a.name), types.StringValue("NO ACTION"))...)
		}
	}
	if m2a {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("constraint_name"), types.StringNull())...)
	}
}

func (r *RelationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data RelationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, diags := relationWriteFromModel(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	body.Collection, body.Field = data.Collection.ValueString(), data.Field.ValueString()
	body.RelatedCollection = data.RelatedCollection.ValueStringPointer()

	if err := r.client.CreateRelation(ctx, body); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create relation %s.%s: %s", body.Collection, body.Field, err))
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RelationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data RelationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.GetRelation(ctx, data.Collection.ValueString(), data.Field.ValueString()); directus.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RelationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data RelationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, diags := relationWriteFromModel(ctx, &data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.UpdateRelation(ctx, data.Collection.ValueString(), data.Field.ValueString(), body); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update relation %s.%s: %s", data.Collection.ValueString(), data.Field.ValueString(), err))
		return
	}
	resp.Diagnostics.Append(r.read(ctx, &data)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RelationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data RelationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteRelation(ctx, data.Collection.ValueString(), data.Field.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete relation %s.%s: %s", data.Collection.ValueString(), data.Field.ValueString(), err))
	}
}

// ImportState takes "<collection>.<field>" (the many side).
func (r *RelationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	collection, field, ok := strings.Cut(req.ID, ".")
	if !ok || collection == "" || field == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected <collection>.<field>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("collection"), collection)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("field"), field)...)
}

// relationWriteFromModel builds meta (always, every managed key) and, for a
// relation with a foreign key, the FK actions.
func relationWriteFromModel(ctx context.Context, m *RelationResourceModel) (*directus.RelationWrite, diag.Diagnostics) {
	var diags diag.Diagnostics
	var allowed []string
	if !m.OneAllowedCollections.IsNull() && !m.OneAllowedCollections.IsUnknown() {
		diags.Append(m.OneAllowedCollections.ElementsAs(ctx, &allowed, false)...)
	}
	body := &directus.RelationWrite{
		Meta: map[string]any{
			"one_field":               m.OneField.ValueStringPointer(),
			"one_collection_field":    m.OneCollectionField.ValueStringPointer(),
			"one_allowed_collections": allowed,
			"junction_field":          m.JunctionField.ValueStringPointer(),
			"sort_field":              m.SortField.ValueStringPointer(),
			"one_deselect_action":     m.OneDeselectAction.ValueString(),
		},
	}
	if !m.RelatedCollection.IsNull() {
		body.Schema = map[string]any{"on_delete": m.OnDelete.ValueString(), "on_update": m.OnUpdate.ValueString()}
	}
	return body, diags
}

// read refreshes everything but collection and field.
func (r *RelationResource) read(ctx context.Context, data *RelationResourceModel) (diags diag.Diagnostics) {
	rel, err := r.client.GetRelation(ctx, data.Collection.ValueString(), data.Field.ValueString())
	if err != nil {
		diags.AddError("Client Error", fmt.Sprintf("Unable to read relation %s.%s: %s", data.Collection.ValueString(), data.Field.ValueString(), err))
		return diags
	}
	data.ID = types.StringValue(rel.Collection + "." + rel.Field)
	data.RelatedCollection = types.StringPointerValue(rel.RelatedCollection)
	data.OnDelete, data.OnUpdate, data.ConstraintName = types.StringNull(), types.StringNull(), types.StringNull()
	if s := rel.Schema; s != nil {
		data.OnDelete = types.StringPointerValue(s.OnDelete)
		data.OnUpdate = types.StringPointerValue(s.OnUpdate)
		data.ConstraintName = types.StringPointerValue(s.ConstraintName)
	}

	m := rel.Meta
	if m == nil {
		// A foreign key created outside Directus has no meta row.
		nullify := "nullify"
		m = &directus.RelationMeta{OneDeselectAction: &nullify}
	}
	data.OneField = types.StringPointerValue(m.OneField)
	data.OneCollectionField = types.StringPointerValue(m.OneCollectionField)
	data.OneAllowedCollections = types.ListNull(types.StringType)
	if len(m.OneAllowedCollections) > 0 {
		l, d := types.ListValueFrom(ctx, types.StringType, m.OneAllowedCollections)
		diags.Append(d...)
		data.OneAllowedCollections = l
	}
	data.JunctionField = types.StringPointerValue(m.JunctionField)
	data.SortField = types.StringPointerValue(m.SortField)
	data.OneDeselectAction = types.StringPointerValue(m.OneDeselectAction)
	return diags
}
