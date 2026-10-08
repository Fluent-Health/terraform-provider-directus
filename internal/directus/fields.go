package directus

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const fieldsPath = "/fields"

// Field is a row of GET /fields. Schema is nil for alias fields (no column);
// Meta is nil for a column with no directus_fields row.
type Field struct {
	Collection string          `json:"collection"`
	Field      string          `json:"field"`
	Type       string          `json:"type"`
	Schema     *FieldSchema    `json:"schema"`
	Meta       json.RawMessage `json:"meta"`
}

// FieldSchema is the column info Directus reads from the database.
type FieldSchema struct {
	DataType         string          `json:"data_type"`
	DefaultValue     json.RawMessage `json:"default_value"`
	MaxLength        *int64          `json:"max_length"`
	NumericPrecision *int64          `json:"numeric_precision"`
	NumericScale     *int64          `json:"numeric_scale"`
	IsNullable       bool            `json:"is_nullable"`
	IsUnique         bool            `json:"is_unique"`
	IsIndexed        bool            `json:"is_indexed"`
	IsPrimaryKey     bool            `json:"is_primary_key"`
	HasAutoIncrement bool            `json:"has_auto_increment"`
	ForeignKeyTable  *string         `json:"foreign_key_table"`
	ForeignKeyColumn *string         `json:"foreign_key_column"`
}

// FieldCreate is a field in a POST /collections or POST /fields body.
type FieldCreate struct {
	Field  string         `json:"field"`
	Type   string         `json:"type"`
	Meta   map[string]any `json:"meta,omitempty"`
	Schema map[string]any `json:"schema"`
}

// ListFields returns every field of every user collection (system collections
// excluded), from the shared per-process listing.
// GET /fields — https://directus.io/docs/api/fields#list-all-fields
func (c *Client) ListFields(ctx context.Context) ([]Field, error) {
	raw, err := c.list(ctx, fieldsPath)
	if err != nil {
		return nil, err
	}
	var all []Field
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decode fields: %w", err)
	}
	out := all[:0]
	for i := range all {
		if !strings.HasPrefix(all[i].Collection, "directus_") {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// PrimaryKeyField returns the primary-key field of a table collection, or
// ErrNotFound.
func (c *Client) PrimaryKeyField(ctx context.Context, collection string) (*Field, error) {
	all, err := c.ListFields(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		f := &all[i]
		if f.Collection == collection && f.Schema != nil && f.Schema.IsPrimaryKey {
			return f, nil
		}
	}
	return nil, ErrNotFound
}
