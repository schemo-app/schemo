package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"

	"schemo/internal/platform"
)

func main() {
	version := flag.String("version", "", "release version, e.g. 0.1.3")
	target := flag.String("platform", "", "stamp only darwin, linux or windows manifests; empty stamps all")
	check := flag.Bool("check", false, "validate the version without modifying files")
	compare := flag.String("compare", "", "compare semantic versions and print -1, 0 or 1 without modifying files")
	print := flag.String("print", "", "print one identity value and exit: name, slug, description, "+
		"scheme")
	flag.Parse()

	if *print != "" {
		value, err := identityValue(*print)
		if err != nil {
			log.Fatalf("manifest: %v", err)
		}
		fmt.Println(value)
		return
	}

	if err := checkVersion(*version); err != nil {
		log.Fatalf("manifest: %v", err)
	}
	if *compare != "" {
		if err := checkVersion(*compare); err != nil {
			log.Fatalf("manifest: comparison version: %v", err)
		}
		fmt.Println(semver.Compare("v"+*version, "v"+*compare))
		return
	}
	if *check {
		return
	}
	manifests, err := platformFiles(".", *version, *target)
	if err != nil {
		log.Fatalf("manifest: %v", err)
	}
	for _, file := range manifests {
		if err := patchFile(file.Path, file.Fields); err != nil {
			log.Fatalf("manifest: %v", err)
		}
		fmt.Println(file.Path)
	}
}

const repoRoot = "../../.."

func identityValue(field string) (string, error) {
	switch field {
	case "name":
		return platform.Name, nil
	case "slug":
		return platform.Slug, nil
	case "description":
		return platform.Description, nil
	case "scheme":
		return platform.Scheme, nil
	default:
		return "", fmt.Errorf("no identity value named %q", field)
	}
}

func checkVersion(version string) error {
	switch {
	case version == "":
		return fmt.Errorf("-version is required, e.g. -version 0.1.3")
	case version == "dev":
		return fmt.Errorf("-version got the dev default; pass the release tag without its leading v")
	case strings.HasPrefix(version, "v"):
		return fmt.Errorf("-version got %q; drop the leading v from the tag", version)
	case !semver.IsValid("v"+version) || semver.Canonical("v"+version) != "v"+version:
		return fmt.Errorf("-version must be a complete semantic version without build metadata: %q", version)
	}
	numeric, _, _ := strings.Cut(version, "-")
	for _, part := range strings.Split(numeric, ".") {
		if _, err := strconv.ParseUint(part, 10, 16); err != nil {
			return fmt.Errorf("-version component %q does not fit Windows version metadata", part)
		}
	}
	return nil
}

func patchFile(path string, fields []Field) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sep := "\n"
	if strings.Contains(string(data), "\r\n") {
		sep = "\r\n"
	}
	lines := strings.Split(string(data), sep)

	for _, field := range fields {
		i, err := locate(lines, field)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		lines[i] = field.render()
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, sep)), 0o644); err != nil {
		return err
	}
	return nil
}

func differences() ([]string, error) {
	var found []string
	for _, file := range files(repoRoot, "") {
		data, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, err
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

		for _, field := range file.Fields {
			if field.Version {
				continue
			}
			i, err := locate(lines, field)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", file.Path, err)
			}
			if want := field.render(); lines[i] != want {
				found = append(found, fmt.Sprintf("%s:%d\n  want %s\n  have %s",
					file.Path, i+1, want, lines[i]))
			}
		}
	}
	return found, nil
}
