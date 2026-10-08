package provider

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/Fluent-Health/terraform-provider-directus/internal/directus"
)

func TestAccFolder_basic(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	// Explicit id: the cross-environment shared-UUID use case.
	id := randomUUID(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckFolderDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccFolderConfig(id, name, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_folder.parent", "id", id),
					resource.TestCheckResourceAttr("directus_folder.parent", "name", name),
					resource.TestCheckNoResourceAttr("directus_folder.parent", "parent"),
					resource.TestCheckResourceAttr("directus_folder.parent", "type", "files"),
					resource.TestCheckResourceAttrSet("directus_folder.child", "id"),
					resource.TestCheckNoResourceAttr("directus_folder.child", "parent"),
				),
			},
			// Rename the parent and move the child under it.
			{
				Config: testAccFolderConfig(id, name+"-renamed", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("directus_folder.parent", "id", id),
					resource.TestCheckResourceAttr("directus_folder.parent", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("directus_folder.child", "parent", id),
				),
			},
			// Move the child back to the root: parent must be cleared (null), not left as-is.
			{
				Config: testAccFolderConfig(id, name+"-renamed", false),
				Check:  resource.TestCheckNoResourceAttr("directus_folder.child", "parent"),
			},
			{
				ResourceName:      "directus_folder.parent",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// TestAccFolder_disappears proves a folder deleted outside Terraform is planned
// for re-creation (Directus answers 403, not 404, for a missing item).
func TestAccFolder_disappears(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc")
	id := randomUUID(t)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckFolderDestroyed,
		Steps: []resource.TestStep{
			{
				Config: testAccFolderConfig(id, name, false),
				Check: func(*terraform.State) error {
					return testAccClient().DeleteFolder(context.Background(), id)
				},
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccFolderConfig(id, name string, nested bool) string {
	parent := "null"
	if nested {
		parent = "directus_folder.parent.id"
	}
	return fmt.Sprintf(`
resource "directus_folder" "parent" {
  id   = %q
  name = %q
}

resource "directus_folder" "child" {
  name   = "%s-child"
  parent = %s
}
`, id, name, name, parent)
}

func testAccClient() *directus.Client {
	return directus.NewClient(os.Getenv("DIRECTUS_URL"), os.Getenv("DIRECTUS_TOKEN"))
}

func testAccCheckFolderDestroyed(s *terraform.State) error {
	c := testAccClient()
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "directus_folder" {
			continue
		}
		_, err := c.GetFolder(context.Background(), rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("folder %s still exists", rs.Primary.ID)
		}
		if !directus.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func randomUUID(t *testing.T) string {
	t.Helper()
	u, err := uuidV4()
	if err != nil {
		t.Fatal(err)
	}
	return u
}
