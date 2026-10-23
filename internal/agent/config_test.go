package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEmbeddedBrowserIDsAreValid(t *testing.T) {
	if _, err := loadBrowserIDs(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeManifestsIncludeBothStoresAndFirefox(t *testing.T) {
	ids := browserIDs{
		Chrome:  "abcdefghijklmnopabcdefghijklmnop",
		Edge:    "ponmlkjihgfedcbaponmlkjihgfedcba",
		Firefox: "test@example.com",
	}
	want := []string{"chrome-extension://" + ids.Chrome + "/", "chrome-extension://" + ids.Edge + "/"}
	chromium := chromiumManifest("/app/schemo", ids)
	if !reflect.DeepEqual(chromium.AllowedOrigins, want) || len(chromium.AllowedExtensions) != 0 {
		t.Fatalf("Chromium manifest: %+v", chromium)
	}
	firefox := firefoxManifest("/app/schemo", ids)
	if !reflect.DeepEqual(firefox.AllowedExtensions, []string{ids.Firefox}) || len(firefox.AllowedOrigins) != 0 {
		t.Fatalf("Firefox manifest: %+v", firefox)
	}
	ids.Edge = ids.Chrome
	if len(ids.origins()) != 1 {
		t.Fatal("shared Chrome and Edge ID was duplicated")
	}
}

func TestMissingStoreIDsDoNotGrantAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, HostName+".json")
	if err := writeManifest(dir, chromiumManifest("/app/schemo", browserIDs{Chrome: "abcdefghijklmnopabcdefghijklmnop"})); err != nil {
		t.Fatal(err)
	}
	if err := writeManifest(dir, chromiumManifest("/app/schemo", browserIDs{})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("obsolete Chromium manifest was not removed: %v", err)
	}
}
