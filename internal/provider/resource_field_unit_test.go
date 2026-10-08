package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func baseFieldModel() FieldResourceModel {
	return FieldResourceModel{
		Type:             types.StringValue("string"),
		DefaultValue:     jsontypes.NewNormalizedValue(`"x"`),
		MaxLength:        types.Int64Value(255),
		NumericPrecision: types.Int64Null(),
		NumericScale:     types.Int64Null(),
		Nullable:         types.BoolValue(true),
		Unique:           types.BoolValue(false),
		Indexed:          types.BoolValue(false),
		Note:             types.StringValue("a"),
	}
}

// Directus ALTERs the column for ANY schema object in a PATCH, so a
// metadata-only edit must not count as a column change.
func TestColumnChanged(t *testing.T) {
	state := baseFieldModel()

	metaOnly := baseFieldModel()
	metaOnly.Note = types.StringValue("b")
	if columnChanged(&metaOnly, &state) {
		t.Error("meta-only edit reported as a column change")
	}

	unknownSize := baseFieldModel()
	unknownSize.MaxLength = types.Int64Unknown()
	if columnChanged(&unknownSize, &state) {
		t.Error("unknown (UseStateForUnknown) size reported as a change")
	}

	for name, mutate := range map[string]func(*FieldResourceModel){
		"type":       func(m *FieldResourceModel) { m.Type = types.StringValue("text") },
		"default":    func(m *FieldResourceModel) { m.DefaultValue = jsontypes.NewNormalizedValue(`"y"`) },
		"max_length": func(m *FieldResourceModel) { m.MaxLength = types.Int64Value(64) },
		"nullable":   func(m *FieldResourceModel) { m.Nullable = types.BoolValue(false) },
		"unique":     func(m *FieldResourceModel) { m.Unique = types.BoolValue(true) },
		"indexed":    func(m *FieldResourceModel) { m.Indexed = types.BoolValue(true) },
	} {
		p := baseFieldModel()
		mutate(&p)
		if !columnChanged(&p, &state) {
			t.Errorf("%s change not detected", name)
		}
	}
}
