package directus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const collectionsPath = "/collections"

// Collection is a row of GET /collections: a table (Schema non-nil) or a
// group, i.e. a folder in the data model with no table (Schema nil).
type Collection struct {
	Collection string            `json:"collection"`
	Meta       *CollectionMeta   `json:"meta"`
	Schema     *CollectionSchema `json:"schema"`
}

// CollectionSchema is the table info Directus returns for a table collection.
// Its contents are informational: Directus ignores `schema` on write except as
// "is this a table".
type CollectionSchema struct {
	Name string `json:"name"`
}

// CollectionMeta is a directus_collections row. Pointer fields are nullable
// columns. `status` is deliberately absent: Directus runs a licence check on
// every write that sends it, and it is licensing state, not configuration.
type CollectionMeta struct {
	Icon                     *string         `json:"icon"`
	Note                     *string         `json:"note"`
	DisplayTemplate          *string         `json:"display_template"`
	Hidden                   bool            `json:"hidden"`
	Singleton                bool            `json:"singleton"`
	Translations             json.RawMessage `json:"translations"`
	ArchiveField             *string         `json:"archive_field"`
	ArchiveAppFilter         bool            `json:"archive_app_filter"`
	ArchiveValue             *string         `json:"archive_value"`
	UnarchiveValue           *string         `json:"unarchive_value"`
	SortField                *string         `json:"sort_field"`
	Accountability           *string         `json:"accountability"`
	Color                    *string         `json:"color"`
	ItemDuplicationFields    []string        `json:"item_duplication_fields"`
	Sort                     *int64          `json:"sort"`
	Group                    *string         `json:"group"`
	Collapse                 string          `json:"collapse"`
	PreviewURL               *string         `json:"preview_url"`
	Versioning               bool            `json:"versioning"`
	AutosaveRevisionInterval *int64          `json:"autosave_revision_interval"`

	// System is true for Directus' own directus_* collections.
	System bool `json:"system,omitempty"`
}

// isSystemCollection reports whether c is one of Directus' own collections.
func isSystemCollection(c *Collection) bool {
	return strings.HasPrefix(c.Collection, "directus_") || (c.Meta != nil && c.Meta.System)
}

// ListCollections returns every user collection (system ones excluded), from
// the shared per-process listing.
// GET /collections — https://directus.io/docs/api/collections#list-collections
func (c *Client) ListCollections(ctx context.Context) ([]Collection, error) {
	raw, err := c.list(ctx, collectionsPath)
	if err != nil {
		return nil, err
	}
	var all []Collection
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decode collections: %w", err)
	}
	out := all[:0]
	for i := range all {
		if !isSystemCollection(&all[i]) {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// GetCollection returns one user collection by name, or ErrNotFound.
func (c *Client) GetCollection(ctx context.Context, name string) (*Collection, error) {
	all, err := c.ListCollections(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Collection == name {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}

// CollectionCreate is the POST /collections body.
type CollectionCreate struct {
	Collection string `json:"collection"`
	// Schema is {} for a table and null for a group. Directus only tests it
	// for truthiness.
	Schema *struct{}       `json:"schema"`
	Meta   *CollectionMeta `json:"meta"`
	// Fields, for a table, must contain the primary key; without one Directus
	// injects an auto-increment integer `id`.
	Fields []FieldCreate `json:"fields,omitempty"`
}

// CreateCollection creates a table or group collection.
// POST /collections — https://directus.io/docs/api/collections#create-a-collection
func (c *Client) CreateCollection(ctx context.Context, body *CollectionCreate) (*Collection, error) {
	out := &Collection{}
	err := c.do(ctx, "POST", collectionsPath, nil, body, out)
	c.invalidateSchema()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateCollectionMeta replaces the managed meta of a collection. Directus'
// PATCH only applies `meta` (the table/group shape is immutable) and upserts
// the meta row, so it also adopts a table created without one.
// PATCH /collections/{collection} — https://directus.io/docs/api/collections#update-a-collection
func (c *Client) UpdateCollectionMeta(ctx context.Context, name string, meta *CollectionMeta) (*Collection, error) {
	out := &Collection{}
	err := c.do(ctx, "PATCH", collectionsPath+"/"+url.PathEscape(name), nil, map[string]any{"meta": meta}, out)
	c.invalidateSchema()
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteCollection drops a collection: for a table, its table and every row.
//
// Directus drops the table before removing inbound foreign keys, so deleting a
// collection that another collection's relation points at fails on Postgres
// ("cannot drop table … because other objects depend on it"), surfaced as a
// bare HTTP 500. The error is annotated.
// DELETE /collections/{collection} — https://directus.io/docs/api/collections#delete-a-collection
func (c *Client) DeleteCollection(ctx context.Context, name string) error {
	err := c.do(ctx, "DELETE", collectionsPath+"/"+url.PathEscape(name), nil, nil, nil)
	c.invalidateSchema()
	// Already gone (deleted outside Terraform) is success.
	err = goneOn403(ctx, err, func(ctx context.Context) error {
		_, err := c.GetCollection(ctx, name)
		return err
	})
	var ae *APIError
	if errors.As(err, &ae) && strings.Contains(ae.Message, "other objects depend on it") {
		return fmt.Errorf("collection %s is still the target of a relation from another collection; delete that relation first: %w", name, err)
	}
	return err
}
