package directus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const foldersPath = "/folders"

// Folder types (directus_folders.type, Directus 12.4+).
const (
	FolderTypeFiles = "files"
	FolderTypeFlows = "flows"
)

// Folder is a row of directus_folders.
type Folder struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Parent *string `json:"parent"`
	Type   string  `json:"type"`
}

// ListFolders returns every folder, from the shared per-process listing.
//
// FoldersService.readByQuery hides type=flows folders from non-admin tokens, so
// only an admin token sees every folder.
// GET /folders — https://directus.io/docs/api/folders#list-folders
func (c *Client) ListFolders(ctx context.Context) ([]Folder, error) {
	raw, err := c.list(ctx, foldersPath)
	if err != nil {
		return nil, err
	}
	var out []Folder
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode folders: %w", err)
	}
	return out, nil
}

// GetFolder returns one folder by id, or ErrNotFound.
func (c *Client) GetFolder(ctx context.Context, id string) (*Folder, error) {
	all, err := c.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}

// CreateFolder creates a folder. A non-empty f.ID is sent, so the folder keeps
// a caller-chosen UUID (identical across environments).
// POST /folders — https://directus.io/docs/api/folders#create-a-folder
func (c *Client) CreateFolder(ctx context.Context, f *Folder) (*Folder, error) {
	body := map[string]any{"name": f.Name, "parent": f.Parent, "type": f.Type}
	if f.ID != "" {
		body["id"] = f.ID
	}
	out := &Folder{}
	err := c.do(ctx, "POST", foldersPath, nil, body, out)
	c.invalidate(foldersPath)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateFolder updates name, parent and type. parent is always sent so that
// moving a folder back to the root (null) is applied.
// PATCH /folders/{id} — https://directus.io/docs/api/folders#update-a-folder
func (c *Client) UpdateFolder(ctx context.Context, f *Folder) (*Folder, error) {
	body := map[string]any{"name": f.Name, "parent": f.Parent, "type": f.Type}
	out := &Folder{}
	err := c.do(ctx, "PATCH", foldersPath+"/"+f.ID, nil, body, out)
	c.invalidate(foldersPath)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteFolder deletes a folder.
//
// Directus does not cascade: deleting a folder that still has subfolders fails
// on the directus_folders_parent_foreign constraint, surfaced as a bare HTTP 500
// INTERNAL_SERVER_ERROR. The error is annotated so it does not read as a
// server fault.
// DELETE /folders/{id} — https://directus.io/docs/api/folders#delete-a-folder
func (c *Client) DeleteFolder(ctx context.Context, id string) error {
	err := c.do(ctx, "DELETE", foldersPath+"/"+id, nil, nil, nil)
	c.invalidate(foldersPath)
	var ae *APIError
	if errors.As(err, &ae) && strings.Contains(ae.Message, "foreign key constraint") {
		return fmt.Errorf("folder %s is still referenced (it has subfolders, or another row points at it); move or delete those first: %w", id, err)
	}
	return err
}
