package provider

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// testAccFieldsConfig declares one field of every type the production schema
// uses (string, text, integer, bigInteger, decimal, boolean, timestamp,
// dateTime, date, uuid, json, csv, alias groups and presentation fields).
// `variant` switches a few settings so a second step can test in-place edits.
func testAccFieldsConfig(coll string, variant bool) string {
	title := `
  type          = "string"
  max_length    = 120
  nullable      = false
  default_value = jsonencode("untitled")
  interface     = "input"
  options       = jsonencode({ trim = true, placeholder = "Title" })
  translations  = jsonencode([{ language = "en-US", translation = "Title" }])
  validation    = jsonencode({ _and = [{ title = { _nempty = true } }] })
  validation_message = "required"
`
	summaryType := "string"
	note := "short summary"
	if variant {
		title = `
  type          = "string"
  max_length    = 200
  nullable      = true
  default_value = jsonencode("draft")
  interface     = "input"
  width         = "half"
  required      = true
`
		summaryType = "text" // string -> text: ALTER in place
		note = "longer summary"
	}

	return fmt.Sprintf(`
resource "directus_collection" "c" {
  collection    = %[1]q
  primary_key   = { type = "uuid" }
  allow_destroy = true
}

locals { c = directus_collection.c.collection }

resource "directus_field" "title" {
  collection    = local.c
  field         = "title"
  allow_destroy = true
  %[2]s
}

resource "directus_field" "summary" {
  collection    = local.c
  field         = "summary"
  type          = %[3]q
  note          = %[4]q
  interface     = "input-multiline"
  allow_destroy = true
}

resource "directus_field" "body" {
  collection    = local.c
  field         = "body"
  type          = "text"
  interface     = "input-rich-text-html"
  allow_destroy = true
}

resource "directus_field" "views" {
  collection    = local.c
  field         = "views"
  type          = "integer"
  unique        = true
  default_value = jsonencode(0)
  allow_destroy = true
}

resource "directus_field" "big" {
  collection    = local.c
  field         = "big"
  type          = "bigInteger"
  allow_destroy = true
}

resource "directus_field" "price" {
  collection        = local.c
  field             = "price"
  type              = "decimal"
  numeric_precision = 10
  numeric_scale     = 2
  default_value     = jsonencode(1.5)
  allow_destroy     = true
}

resource "directus_field" "published" {
  collection    = local.c
  field         = "published"
  type          = "boolean"
  special       = ["cast-boolean"]
  default_value = jsonencode(false)
  interface     = "boolean"
  allow_destroy = true
}

resource "directus_field" "date_created" {
  collection    = local.c
  field         = "date_created"
  type          = "timestamp"
  special       = ["date-created"]
  interface     = "datetime"
  readonly      = true
  hidden        = true
  display       = "datetime"
  display_options = jsonencode({ relative = true })
  allow_destroy = true
}

resource "directus_field" "reviewed_at" {
  collection    = local.c
  field         = "reviewed_at"
  type          = "timestamp"
  default_value = jsonencode("CURRENT_TIMESTAMP")
  allow_destroy = true
}

resource "directus_field" "scheduled" {
  collection    = local.c
  field         = "scheduled"
  type          = "dateTime"
  allow_destroy = true
}

resource "directus_field" "day" {
  collection    = local.c
  field         = "day"
  type          = "date"
  indexed       = true
  allow_destroy = true
}

resource "directus_field" "image" {
  collection    = local.c
  field         = "image"
  type          = "uuid"
  special       = ["file"]
  interface     = "file-image"
  allow_destroy = true
}

resource "directus_field" "data" {
  collection    = local.c
  field         = "data"
  type          = "json"
  special       = ["cast-json"]
  default_value = jsonencode({ a = 1 })
  interface     = "input-code"
  options       = jsonencode({ language = "json" })
  allow_destroy = true
}

resource "directus_field" "tags" {
  collection    = local.c
  field         = "tags"
  type          = "csv"
  special       = ["cast-csv"]
  interface     = "tags"
  allow_destroy = true
}

resource "directus_field" "meta_group" {
  collection    = local.c
  field         = "meta_group"
  type          = "alias"
  special       = ["alias", "no-data", "group"]
  interface     = "group-detail"
  options       = jsonencode({ start = "closed" })
  allow_destroy = true
}

resource "directus_field" "seo_title" {
  collection    = local.c
  field         = "seo_title"
  type          = "string"
  group         = directus_field.meta_group.field
  conditions    = jsonencode([{ name = "hide", rule = { title = { _null = true } }, hidden = true }])
  allow_destroy = true
}

resource "directus_field" "divider" {
  collection    = local.c
  field         = "divider"
  type          = "alias"
  special       = ["alias", "no-data"]
  interface     = "presentation-divider"
  options       = jsonencode({ title = "Advanced" })
  allow_destroy = true
}
`, coll, title, summaryType, note)
}

func TestAccField_roundTrip(t *testing.T) {
	coll := testAccCollectionName()

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			// Create every type; the framework then requires an empty plan,
			// i.e. create -> read -> no diff.
			{
				Config: testAccFieldsConfig(coll, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_field.title", "data_type", "character varying"),
					resource.TestCheckResourceAttr("directus_field.title", "max_length", "120"),
					resource.TestCheckResourceAttr("directus_field.title", "nullable", "false"),
					resource.TestCheckResourceAttr("directus_field.title", "default_value", `"untitled"`),
					resource.TestCheckResourceAttr("directus_field.summary", "max_length", "255"),
					resource.TestCheckResourceAttr("directus_field.views", "unique", "true"),
					resource.TestCheckResourceAttr("directus_field.views", "default_value", "0"),
					resource.TestCheckResourceAttr("directus_field.price", "numeric_precision", "10"),
					resource.TestCheckResourceAttr("directus_field.published", "default_value", "false"),
					resource.TestCheckResourceAttr("directus_field.reviewed_at", "default_value", `"CURRENT_TIMESTAMP"`),
					resource.TestCheckResourceAttr("directus_field.day", "indexed", "true"),
					resource.TestCheckResourceAttr("directus_field.meta_group", "type", "alias"),
					resource.TestCheckNoResourceAttr("directus_field.meta_group", "nullable"),
					resource.TestCheckNoResourceAttr("directus_field.meta_group", "data_type"),
					resource.TestCheckResourceAttr("directus_field.seo_title", "group", "meta_group"),
					resource.TestCheckResourceAttrSet("directus_field.title", "sort"),
				),
			},
			// In-place edits: meta, column settings, and a string -> text type change.
			{
				Config: testAccFieldsConfig(coll, true),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("directus_field.title", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("directus_field.summary", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_field.title", "max_length", "200"),
					resource.TestCheckResourceAttr("directus_field.title", "nullable", "true"),
					resource.TestCheckResourceAttr("directus_field.title", "default_value", `"draft"`),
					resource.TestCheckResourceAttr("directus_field.title", "width", "half"),
					resource.TestCheckResourceAttr("directus_field.title", "required", "true"),
					resource.TestCheckNoResourceAttr("directus_field.title", "options"),
					resource.TestCheckNoResourceAttr("directus_field.title", "validation"),
					resource.TestCheckResourceAttr("directus_field.summary", "type", "text"),
					resource.TestCheckResourceAttr("directus_field.summary", "data_type", "text"),
				),
			},
			// Import of existing fields produces no diff.
			testAccFieldImportStep("title", coll),
			testAccFieldImportStep("data", coll),
			testAccFieldImportStep("meta_group", coll),
			testAccFieldImportStep("seo_title", coll),
		},
	})
}

func testAccFieldImportStep(name, coll string) resource.TestStep {
	return resource.TestStep{
		ResourceName:                         "directus_field." + name,
		ImportState:                          true,
		ImportStateId:                        coll + "." + name,
		ImportStateVerify:                    true,
		ImportStateVerifyIdentifierAttribute: "field",
		ImportStateVerifyIgnore:              []string{"allow_destroy"},
	}
}

// TestAccField_guard: removing a field, or moving it across the alias/column
// line, is refused at plan time unless allow_destroy was applied.
func TestAccField_guard(t *testing.T) {
	coll := testAccCollectionName()
	cfg := func(fieldBlock string) string {
		return fmt.Sprintf(`
resource "directus_collection" "c" {
  collection    = %q
  allow_destroy = true
}
%s
`, coll, fieldBlock)
	}
	field := func(typ, extra string) string {
		return fmt.Sprintf(`
resource "directus_field" "f" {
  collection = directus_collection.c.collection
  field      = "f"
  type       = %q
  %s
}
`, typ, extra)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckCollectionsDestroyed,
		Steps: []resource.TestStep{
			{Config: cfg(field("string", ""))},
			{
				Config:      cfg(field("alias", `special = ["alias", "no-data"]`)),
				ExpectError: regexp.MustCompile(`Refusing to replace directus_field`),
			},
			{
				Config:      cfg(""),
				ExpectError: regexp.MustCompile(`Refusing to destroy directus_field`),
			},
			{
				Config: cfg(field("string", "")),
				Check: func(*terraform.State) error {
					_, err := testAccClient().GetField(context.Background(), coll, "f")
					return err
				},
			},
			{Config: cfg(field("string", "allow_destroy = true"))},
			{Config: cfg("")},
		},
	})
}

func TestAccField_validation(t *testing.T) {
	coll := testAccCollectionName()
	for _, tc := range []struct {
		block string
		want  string
	}{
		{`type = "csv"`, `needs "cast-csv" in special`},
		{`type = "json"`, `needs "cast-json" in special`},
		{"type = \"alias\"\n  special = [\"group\"]", `Alias field is not listable`},
		{"type = \"float\"\n  numeric_precision = 10", `Precision only applies to decimal`},
		{"type = \"timestamp\"\n  default_value = jsonencode(\"now()\")", `Use CURRENT_TIMESTAMP`},
	} {
		resource.Test(t, resource.TestCase{
			PreCheck:                 func() { testAccPreCheck(t) },
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{{
				Config: fmt.Sprintf(`
resource "directus_field" "f" {
  collection = %q
  field      = "f"
  %s
}
`, coll, tc.block),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(regexp.QuoteMeta(tc.want)),
			}},
		})
	}
}
