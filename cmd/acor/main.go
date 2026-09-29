// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	app "github.com/skyoo2003/acor/internal/app/acor"
)

// version is stamped at build time with -ldflags "-X main.version=vX.Y.Z". A
// plain go build or go install leaves it "dev".
var version = "dev"

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}
