package directus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const relationsPath = "/relations"

// Relation is a row of GET /relations, keyed by its many side
// (collection.field). Schema is the foreign key, nil for an m2a relation
// (no FK); Meta is nil for a foreign key created outside Directus.
type Relation struct {
	Collection        string          `json:"collection"`
	Field             string          `json:"field"`
	RelatedCollection *string         `json:"related_collection"`
	Schema            *RelationSchema `json:"schema"`
	Meta              *RelationMeta   `json:"meta"`
}

// RelationSchema is the foreign key as Directus reads it from the database.
type RelationSchema struct {
	ConstraintName *string `json:"constraint_name"`
	OnDelete       *string `json:"on_delete"`
	OnUpdate       *string `json:"on_update"`
}

// RelationMeta is a directus_relations row.
type RelationMeta struct {
	OneField              *string  `json:"one_field"`
	OneCollectionField    *string  `json:"one_collection_field"`
	OneAllowedCollections []string `json:"one_allowed_collections"`
	JunctionField         *string  `json:"junction_field"`
	SortField             *string  `json:"sort_field"`
	OneDeselectAction     *string  `json:"one_deselect_action"`
	System                bool     `json:"system,omitempty"`
}

// ListRelations returns every user relation (system ones excluded), from the
// shared per-process listing.
// GET /relations — https://directus.io/docs/api/relations#list-relations
func (c *Client) ListRelations(ctx context.Context) ([]Relation, error) {
	raw, err := c.list(ctx, relationsPath)
	if err != nil {
		return nil, err
	}
	var all []Relation
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decode relations: %w", err)
	}
	out := all[:0]
	for i := range all {
		r := &all[i]
		if strings.HasPrefix(r.Collection, "directus_") || (r.Meta != nil && r.Meta.System) {
			continue
		}
		out = append(out, *r)
	}
	return out, nil
}

// GetRelation returns the relation on collection.field, or ErrNotFound.
func (c *Client) GetRelation(ctx context.Context, collection, field string) (*Relation, error) {
	all, err := c.ListRelations(ctx)
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

// RelationWrite is a POST or PATCH /relations body. Schema nil is omitted
// (m2a relations have no foreign key).
type RelationWrite struct {
	Collection        string         `json:"collection,omitempty"`
	Field             string         `json:"field,omitempty"`
	RelatedCollection *string        `json:"related_collection,omitempty"`
	Schema            map[string]any `json:"schema,omitempty"`
	Meta              map[string]any `json:"meta"`
}

// CreateRelation creates a relation and, unless it is an m2a, its foreign key.
// The many-side field must already exist.
// POST /relations — https://directus.io/docs/api/relations#create-a-relation
func (c *Client) CreateRelation(ctx context.Context, body *RelationWrite) error {
	err := c.do(ctx, "POST", relationsPath, nil, body, nil)
	c.invalidateSchema()
	return err
}

// UpdateRelation updates meta and, for a relation with a foreign key, its
// on_delete/on_update (Directus drops and re-creates the constraint). The
// related collection cannot change: Directus keeps the existing one.
// PATCH /relations/{collection}/{field} — https://directus.io/docs/api/relations#update-a-relation
func (c *Client) UpdateRelation(ctx context.Context, collection, field string, body *RelationWrite) error {
	err := c.do(ctx, "PATCH", relationsPath+"/"+url.PathEscape(collection)+"/"+url.PathEscape(field), nil, body, nil)
	c.invalidateSchema()
	return err
}

// DeleteRelation drops the foreign key and the directus_relations row. The
// field and its data stay. Deleting an m2o field already removes its relation,
// after which Directus answers 400 "doesn't exist": that is success.
// DELETE /relations/{collection}/{field} — https://directus.io/docs/api/relations#delete-a-relation
func (c *Client) DeleteRelation(ctx context.Context, collection, field string) error {
	err := c.do(ctx, "DELETE", relationsPath+"/"+url.PathEscape(collection)+"/"+url.PathEscape(field), nil, nil, nil)
	c.invalidateSchema()
	return goneIfAbsent(ctx, err, func(ctx context.Context) error {
		_, err := c.GetRelation(ctx, collection, field)
		return err
	})
}
