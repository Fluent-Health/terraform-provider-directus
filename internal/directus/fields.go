package directus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

// AliasSpecials are the meta.special flags that make Directus list an alias
// field. GET /fields only lists an alias field (no column) whose special
// contains one of these; one with e.g. only ["group"] is invisible to the
// listing, so the provider would think it was deleted.
var AliasSpecials = []string{"alias", "o2m", "m2m", "m2a", "o2a", "files", "translations"}

// FieldMeta is a directus_fields row as read back.
type FieldMeta struct {
	Special           []string        `json:"special"`
	Interface         *string         `json:"interface"`
	Options           json.RawMessage `json:"options"`
	Display           *string         `json:"display"`
	DisplayOptions    json.RawMessage `json:"display_options"`
	Readonly          bool            `json:"readonly"`
	Hidden            bool            `json:"hidden"`
	Sort              *int64          `json:"sort"`
	Width             *string         `json:"width"`
	Translations      json.RawMessage `json:"translations"`
	Note              *string         `json:"note"`
	Conditions        json.RawMessage `json:"conditions"`
	Required          bool            `json:"required"`
	Group             *string         `json:"group"`
	Validation        json.RawMessage `json:"validation"`
	ValidationMessage *string         `json:"validation_message"`
	Searchable        bool            `json:"searchable"`
}

// ParsedMeta decodes Meta; nil when the field has no directus_fields row.
func (f *Field) ParsedMeta() (*FieldMeta, error) {
	if len(f.Meta) == 0 || string(f.Meta) == "null" {
		return nil, nil
	}
	m := &FieldMeta{}
	if err := json.Unmarshal(f.Meta, m); err != nil {
		return nil, fmt.Errorf("decode meta of field %s.%s: %w", f.Collection, f.Field, err)
	}
	return m, nil
}

// GetField returns one user field, or ErrNotFound.
func (c *Client) GetField(ctx context.Context, collection, field string) (*Field, error) {
	all, err := c.ListFields(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Collection == collection && all[i].Field == field {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}

// FieldWrite is a POST or PATCH /fields body. Schema nil is omitted: on PATCH
// that leaves the column alone (any schema object makes Directus ALTER the
// column, even an unchanged one), and alias fields have no column.
type FieldWrite struct {
	Field  string         `json:"field,omitempty"`
	Type   string         `json:"type"`
	Meta   map[string]any `json:"meta"`
	Schema map[string]any `json:"schema,omitempty"`
}

// CreateField adds a field (and, unless it is an alias, its column).
// POST /fields/{collection} — https://directus.io/docs/api/fields#create-a-field
func (c *Client) CreateField(ctx context.Context, collection string, body *FieldWrite) error {
	err := c.do(ctx, "POST", fieldsPath+"/"+url.PathEscape(collection), nil, body, nil)
	c.invalidateSchema()
	return err
}

// UpdateField updates a field. A type change takes effect only together with
// a schema object (Directus derives the type from the column, and only ALTERs
// it when schema is sent).
// PATCH /fields/{collection}/{field} — https://directus.io/docs/api/fields#update-a-field
func (c *Client) UpdateField(ctx context.Context, collection, field string, body *FieldWrite) error {
	err := c.do(ctx, "PATCH", fieldsPath+"/"+url.PathEscape(collection)+"/"+url.PathEscape(field), nil, body, nil)
	c.invalidateSchema()
	return err
}

// DeleteField drops a field: its column and every value in it, its relation
// if it is an m2o, and the o2m alias on the other side of that relation.
// DELETE /fields/{collection}/{field} — https://directus.io/docs/api/fields#delete-a-field
func (c *Client) DeleteField(ctx context.Context, collection, field string) error {
	err := c.do(ctx, "DELETE", fieldsPath+"/"+url.PathEscape(collection)+"/"+url.PathEscape(field), nil, nil, nil)
	c.invalidateSchema()
	return goneOn403(ctx, err, func(ctx context.Context) error {
		_, err := c.GetField(ctx, collection, field)
		return err
	})
}
