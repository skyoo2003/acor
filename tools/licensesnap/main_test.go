// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("LICENSESNAP_GO_HELPER") == "1" {
		fmt.Printf("example.com/dependency\tv1.0.0\t%s\n",
			os.Getenv("GOOS")+"/"+os.Getenv("GOARCH")+
				" CGO_ENABLED="+os.Getenv("CGO_ENABLED")+
				" GO386="+os.Getenv("GO386")+
				" GOAMD64="+os.Getenv("GOAMD64")+
				" GOARM="+os.Getenv("GOARM")+
				" GOARM64="+os.Getenv("GOARM64"))
		return
	}
	os.Exit(m.Run())
}

func TestListModulesPinsReleaseEnvironment(t *testing.T) {
	dir := t.TempDir()
	fakeGo := filepath.Join(dir, "go")
	if runtime.GOOS == "windows" {
		fakeGo += ".exe"
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if linkErr := os.Link(testBinary, fakeGo); linkErr != nil {
		t.Fatal(linkErr)
	}
	t.Setenv("PATH", dir)
	t.Setenv("LICENSESNAP_GO_HELPER", "1")
	t.Setenv("CGO_ENABLED", "1")
	t.Setenv("GO386", "softfloat")
	t.Setenv("GOAMD64", "v4")
	t.Setenv("GOARM", "6")
	t.Setenv("GOARM64", "v9.5")

	mods, err := listModules("linux", "arm")
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) != 1 {
		t.Fatalf("listModules returned %d modules, want 1", len(mods))
	}
	const want = "linux/arm CGO_ENABLED=0 GO386=sse2 GOAMD64=v1 GOARM=7 GOARM64=v8.0"
	if got := mods[0].dir; got != want {
		t.Errorf("go command environment = %q, want %q", got, want)
	}
}
