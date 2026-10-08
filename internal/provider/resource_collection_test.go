package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/Fluent-Health/terraform-provider-directus/internal/directus"
)

// testAccCollectionName returns a random lower-case collection name.
func testAccCollectionName() string {
	return strings.ReplaceAll(acctest.RandomWithPrefix("tf_acc"), "-", "_")
}

func TestAccCollection_table(t *testing.T) {
	group := testAccCollectionName()
	name := testAccCollectionName()

	cfg := func(icon, extra string) string {
		return fmt.Sprintf(`
resource "directus_collection" "group" {
  collection    = %[1]q
  table         = false
  collapse      = "closed"
  allow_destroy = true
}

resource "directus_collection" "test" {
  collection    = %[2]q
  primary_key   = { type = "uuid" }
  group         = directus_collection.group.collection
  icon          = %[3]q
  translations  = jsonencode([{ language = "en-US", translation = "Test", singular = "Test", plural = "Tests" }])
  allow_destroy = true
  %[4]s
}
`, group, name, icon, extra)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			{
				Config: cfg("box", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_collection.group", "table", "false"),
					resource.TestCheckNoResourceAttr("directus_collection.group", "primary_key"),
					resource.TestCheckResourceAttr("directus_collection.group", "collapse", "closed"),
					resource.TestCheckResourceAttr("directus_collection.test", "table", "true"),
					resource.TestCheckResourceAttr("directus_collection.test", "primary_key.field", "id"),
					resource.TestCheckResourceAttr("directus_collection.test", "primary_key.type", "uuid"),
					resource.TestCheckResourceAttr("directus_collection.test", "group", group),
					resource.TestCheckResourceAttr("directus_collection.test", "accountability", "all"),
					resource.TestCheckResourceAttr("directus_collection.test", "archive_app_filter", "true"),
				),
			},
			// In-place meta update: no replacement, so the guard stays silent.
			{
				Config: cfg("article", `
  note                    = "managed"
  hidden                  = true
  accountability          = "activity"
  item_duplication_fields = ["id"]
  sort                    = 3
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_collection.test", "icon", "article"),
					resource.TestCheckResourceAttr("directus_collection.test", "note", "managed"),
					resource.TestCheckResourceAttr("directus_collection.test", "hidden", "true"),
					resource.TestCheckResourceAttr("directus_collection.test", "accountability", "activity"),
					resource.TestCheckResourceAttr("directus_collection.test", "item_duplication_fields.0", "id"),
					resource.TestCheckResourceAttr("directus_collection.test", "sort", "3"),
				),
			},
			// Removing optional settings returns them to Directus' defaults.
			{
				Config: cfg("article", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("directus_collection.test", "note"),
					resource.TestCheckResourceAttr("directus_collection.test", "hidden", "false"),
					resource.TestCheckResourceAttr("directus_collection.test", "accountability", "all"),
					resource.TestCheckNoResourceAttr("directus_collection.test", "item_duplication_fields"),
				),
			},
			{
				ResourceName:                         "directus_collection.test",
				ImportState:                          true,
				ImportStateId:                        name,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "collection",
				ImportStateVerifyIgnore:              []string{"allow_destroy"},
			},
		},
	})
}

// TestAccCollection_defaultPrimaryKey: no primary_key block gives Directus'
// auto-increment integer id, and the plan stays empty afterwards.
func TestAccCollection_defaultPrimaryKey(t *testing.T) {
	name := testAccCollectionName()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "directus_collection" "test" {
  collection    = %q
  allow_destroy = true
}
`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_collection.test", "primary_key.field", "id"),
					resource.TestCheckResourceAttr("directus_collection.test", "primary_key.type", "integer"),
				),
			},
		},
	})
}

// TestAccCollection_destroyGuard: with allow_destroy unset, both removing the
// collection and a replacing change fail at PLAN time, and the table survives.
// Opting in then lets it go.
func TestAccCollection_destroyGuard(t *testing.T) {
	name := testAccCollectionName()
	guarded := func(pkType string) string {
		return fmt.Sprintf(`
resource "directus_collection" "test" {
  collection  = %q
  primary_key = { type = %q }
}
`, name, pkType)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			{
				Config: guarded("integer"),
				Check:  resource.TestCheckResourceAttr("directus_collection.test", "allow_destroy", "false"),
			},
			{
				Config:      guarded("uuid"),
				ExpectError: regexp.MustCompile(`Refusing to replace directus_collection`),
			},
			{
				Config:      `# collection removed from config`,
				ExpectError: regexp.MustCompile(`Refusing to destroy directus_collection`),
			},
			{
				// Still there after both refusals.
				Config: guarded("integer"),
				Check: func(*terraform.State) error {
					_, err := testAccClient().GetCollection(context.Background(), name)
					return err
				},
			},
			// Opting in, then removing, works.
			{
				Config: fmt.Sprintf(`
resource "directus_collection" "test" {
  collection    = %q
  primary_key   = { type = "integer" }
  allow_destroy = true
}
`, name),
			},
			{
				Config: `# collection removed from config`,
			},
		},
	})
}

func TestAccCollection_disappears(t *testing.T) {
	name := testAccCollectionName()
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "directus_collection" "test" {
  collection    = %q
  allow_destroy = true
}
`, name),
				Check: func(*terraform.State) error {
					return testAccClient().DeleteCollection(context.Background(), name)
				},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccCheckCollectionsDestroyed(s *terraform.State) error {
	c := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "directus_collection" {
			continue
		}
		name := rs.Primary.Attributes["collection"]
		_, err := c.GetCollection(context.Background(), name)
		if err == nil {
			return fmt.Errorf("collection %s still exists", name)
		}
		if !directus.IsNotFound(err) {
			return err
		}
	}
	return nil
}
