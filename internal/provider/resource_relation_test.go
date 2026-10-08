package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccRelationsConfig builds every relational shape the production schema
// uses: m2o (+ its o2m alias), m2o to directus_files, m2m through a junction,
// and m2a over two block collections.
func testAccRelationsConfig(p string, authorOnDelete string) string {
	return fmt.Sprintf(`
locals { p = %[1]q }

resource "directus_collection" "authors" {
  collection    = "${local.p}_authors"
  primary_key   = { type = "uuid" }
  allow_destroy = true
}
resource "directus_collection" "articles" {
  collection    = "${local.p}_articles"
  primary_key   = { type = "uuid" }
  allow_destroy = true
}
resource "directus_collection" "tags" {
  collection    = "${local.p}_tags"
  allow_destroy = true
}
resource "directus_collection" "articles_tags" {
  collection    = "${local.p}_articles_tags"
  hidden        = true
  allow_destroy = true
}
resource "directus_collection" "block_text" {
  collection    = "${local.p}_block_text"
  allow_destroy = true
}
resource "directus_collection" "block_image" {
  collection    = "${local.p}_block_image"
  allow_destroy = true
}
resource "directus_collection" "articles_blocks" {
  collection    = "${local.p}_articles_blocks"
  hidden        = true
  allow_destroy = true
}

# --- m2o articles.author -> authors, with the o2m authors.articles back ---
resource "directus_field" "author" {
  collection    = directus_collection.articles.collection
  field         = "author"
  type          = "uuid"
  special       = ["m2o"]
  interface     = "select-dropdown-m2o"
  options       = jsonencode({ template = "{{id}}" })
  allow_destroy = true
}
resource "directus_field" "authors_articles" {
  collection    = directus_collection.authors.collection
  field         = "articles"
  type          = "alias"
  special       = ["o2m"]
  interface     = "list-o2m"
  allow_destroy = true
}
resource "directus_relation" "author" {
  collection         = directus_field.author.collection
  field              = directus_field.author.field
  related_collection = directus_collection.authors.collection
  one_field          = directus_field.authors_articles.field
  on_delete          = %[2]q
}

# --- m2o articles.cover -> directus_files ---
resource "directus_field" "cover" {
  collection    = directus_collection.articles.collection
  field         = "cover"
  type          = "uuid"
  special       = ["file"]
  interface     = "file-image"
  allow_destroy = true
}
resource "directus_relation" "cover" {
  collection         = directus_field.cover.collection
  field              = directus_field.cover.field
  related_collection = "directus_files"
  on_delete          = "SET NULL"
}

# --- m2m articles.tags <-> tags via articles_tags ---
resource "directus_field" "tags" {
  collection    = directus_collection.articles.collection
  field         = "tags"
  type          = "alias"
  special       = ["m2m"]
  interface     = "list-m2m"
  allow_destroy = true
}
resource "directus_field" "at_article" {
  collection    = directus_collection.articles_tags.collection
  field         = "articles_id"
  type          = "uuid"
  hidden        = true
  allow_destroy = true
}
resource "directus_field" "at_tag" {
  collection    = directus_collection.articles_tags.collection
  field         = "tags_id"
  type          = "integer"
  hidden        = true
  allow_destroy = true
}
resource "directus_relation" "at_article" {
  collection         = directus_field.at_article.collection
  field              = directus_field.at_article.field
  related_collection = directus_collection.articles.collection
  one_field          = directus_field.tags.field
  junction_field     = directus_field.at_tag.field
  on_delete          = "CASCADE"
}
resource "directus_relation" "at_tag" {
  collection         = directus_field.at_tag.collection
  field              = directus_field.at_tag.field
  related_collection = directus_collection.tags.collection
  junction_field     = directus_field.at_article.field
  on_delete          = "CASCADE"
}

# --- m2a articles.blocks -> block_text | block_image via articles_blocks ---
resource "directus_field" "blocks" {
  collection    = directus_collection.articles.collection
  field         = "blocks"
  type          = "alias"
  special       = ["m2a"]
  interface     = "list-m2a"
  allow_destroy = true
}
resource "directus_field" "ab_article" {
  collection    = directus_collection.articles_blocks.collection
  field         = "articles_id"
  type          = "uuid"
  hidden        = true
  allow_destroy = true
}
resource "directus_field" "ab_item" {
  collection    = directus_collection.articles_blocks.collection
  field         = "item"
  type          = "string"
  hidden        = true
  allow_destroy = true
}
resource "directus_field" "ab_collection" {
  collection    = directus_collection.articles_blocks.collection
  field         = "collection"
  type          = "string"
  hidden        = true
  allow_destroy = true
}
resource "directus_field" "ab_sort" {
  collection    = directus_collection.articles_blocks.collection
  field         = "sort"
  type          = "integer"
  hidden        = true
  allow_destroy = true
}
resource "directus_relation" "ab_item" {
  collection              = directus_field.ab_item.collection
  field                   = directus_field.ab_item.field
  one_allowed_collections = [directus_collection.block_text.collection, directus_collection.block_image.collection]
  one_collection_field    = directus_field.ab_collection.field
  junction_field          = directus_field.ab_article.field
}
resource "directus_relation" "ab_article" {
  collection         = directus_field.ab_article.collection
  field              = directus_field.ab_article.field
  related_collection = directus_collection.articles.collection
  one_field          = directus_field.blocks.field
  junction_field     = directus_field.ab_item.field
  sort_field         = directus_field.ab_sort.field
  on_delete          = "CASCADE"
}
`, p, authorOnDelete)
}

func TestAccRelation_shapes(t *testing.T) {
	p := testAccCollectionName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			// Create every shape; the plan must be empty afterwards.
			{
				Config: testAccRelationsConfig(p, "SET NULL"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_relation.author", "on_delete", "SET NULL"),
					resource.TestCheckResourceAttr("directus_relation.author", "on_update", "NO ACTION"),
					resource.TestCheckResourceAttrSet("directus_relation.author", "constraint_name"),
					resource.TestCheckResourceAttr("directus_relation.author", "one_field", "articles"),
					resource.TestCheckResourceAttr("directus_relation.cover", "related_collection", "directus_files"),
					resource.TestCheckResourceAttr("directus_relation.at_article", "junction_field", "tags_id"),
					resource.TestCheckNoResourceAttr("directus_relation.ab_item", "related_collection"),
					resource.TestCheckNoResourceAttr("directus_relation.ab_item", "on_delete"),
					resource.TestCheckNoResourceAttr("directus_relation.ab_item", "constraint_name"),
					resource.TestCheckResourceAttr("directus_relation.ab_item", "one_allowed_collections.#", "2"),
					resource.TestCheckResourceAttr("directus_relation.ab_article", "sort_field", "sort"),
				),
			},
			// on_delete changes in place (Directus re-creates the constraint).
			{
				Config: testAccRelationsConfig(p, "CASCADE"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("directus_relation.author", plancheck.ResourceActionUpdate),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_relation.author", "on_delete", "CASCADE"),
					// The field was created before its relation; after a
					// refresh it reports the foreign key the relation made.
					resource.TestCheckResourceAttr("directus_field.author", "foreign_key_table", p+"_authors"),
				),
			},
			testAccRelationImportStep("author", p+"_articles.author"),
			testAccRelationImportStep("at_article", p+"_articles_tags.articles_id"),
			testAccRelationImportStep("ab_item", p+"_articles_blocks.item"),
		},
	})
}

func testAccRelationImportStep(name, id string) resource.TestStep {
	return resource.TestStep{
		ResourceName:      "directus_relation." + name,
		ImportState:       true,
		ImportStateId:     id,
		ImportStateVerify: true,
	}
}

// TestAccRelation_fieldDeletedFirst: deleting the m2o field removes its
// relation inside Directus; the relation resource must then plan a re-create,
// not fail.
func TestAccRelation_fieldDeletedFirst(t *testing.T) {
	p := testAccCollectionName()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccRelationsConfig(p, "SET NULL"),
				Check: func(*terraform.State) error {
					c := testAccClient()
					if err := c.DeleteField(context.Background(), p+"_articles", "author"); err != nil {
						return err
					}
					_, err := c.GetRelation(context.Background(), p+"_articles", "author")
					if err == nil {
						return fmt.Errorf("relation survived its field")
					}
					return nil
				},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
