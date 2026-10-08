package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// allowDestroyAttribute is the destroy-guard opt-in shared by the data-model
// resources. Destroying a collection or field drops its table or column and
// every value in it; there is no undo.
func allowDestroyAttribute(what string) schema.BoolAttribute {
	return schema.BoolAttribute{
		Optional: true,
		Computed: true,
		Default:  booldefault.StaticBool(false),
		MarkdownDescription: fmt.Sprintf("Allow Terraform to destroy this %s, including replacing it. Defaults to `false`: "+
			"a plan that would destroy or replace it fails. Set `allow_destroy = true` and apply that first, then remove or replace the resource. "+
			"Like `lifecycle.prevent_destroy`, but enforced by the provider and off by default.", what),
	}
}

// guardDestroy fails the plan when it would destroy or replace an object whose
// allow_destroy is false. replacing must be computed by the caller from its
// replace-forcing attributes: the framework hands ModifyPlan a fresh, empty
// resp.RequiresReplace (fwserver/server_planresourcechange.go), so the
// attribute-level RequiresReplace results are not visible here.
//
// The opt-in is always read from STATE, so it must be applied before the
// destroying change: a destroy has no plan to read, and a replacement's destroy
// half runs against the prior state (Terraform plans and applies that delete
// with the old allow_destroy), so a same-change opt-in could never work.
func guardDestroy(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, kind, name, consequence string, replacing []string) {
	if req.State.Raw.IsNull() {
		return // create
	}

	var allow types.Bool
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("allow_destroy"), &allow)...)
	if resp.Diagnostics.HasError() || allow.ValueBool() {
		return
	}
	switch {
	case req.Plan.Raw.IsNull():
		resp.Diagnostics.AddError(
			fmt.Sprintf("Refusing to destroy %s %q", kind, name),
			fmt.Sprintf("Destroying it %s. To go ahead, set allow_destroy = true on it and apply that first, then remove it.", consequence),
		)
	case len(replacing) > 0:
		resp.Diagnostics.AddError(
			fmt.Sprintf("Refusing to replace %s %q", kind, name),
			fmt.Sprintf("Changing %s forces a replacement, and destroying it %s. To go ahead, set allow_destroy = true on it and apply that first, then make this change.", strings.Join(replacing, ", "), consequence),
		)
	}
}
