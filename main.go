package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/Fluent-Health/terraform-provider-directus/internal/provider"
)

// Format example terraform files and generate the registry docs.
//go:generate terraform fmt -recursive ./examples/
//go:generate env GOTOOLCHAIN=go1.25.8 go run -modfile=tools/go.mod github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate -provider-name directus

// version is set by the release build via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to run the provider with debugger support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/Fluent-Health/directus",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
