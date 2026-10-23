package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"schemo/internal/platform"
)

type Field struct {
	Value   string
	Find    string
	Offset  int
	Format  string
	Render  func() string
	Version bool
}

func (f Field) render() string {
	if f.Render != nil {
		return f.Render()
	}
	return fmt.Sprintf(f.Format, f.Value)
}

type File struct {
	Path   string
	Fields []Field
}

func platformFiles(root, version, target string) ([]File, error) {
	switch target {
	case "", "darwin", "linux", "windows":
	default:
		return nil, fmt.Errorf("unsupported platform %q", target)
	}
	manifests := files(root, version)
	if target == "" {
		return manifests, nil
	}
	build := filepath.Join(root, "build")
	prefix := filepath.Join(build, target) + string(filepath.Separator)
	return slices.DeleteFunc(manifests, func(file File) bool {
		return file.Path != filepath.Join(build, "config.yml") && !strings.HasPrefix(file.Path, prefix)
	}), nil
}

func files(root, version string) []File {
	numeric, _, _ := strings.Cut(version, "-")
	plist := func(path string) File {
		return File{Path: filepath.Join(root, path), Fields: []Field{
			{Value: platform.Slug, Find: "<key>CFBundleExecutable</key>", Offset: 1,
				Format: "\t\t<string>%s</string>"},
			{Value: platform.Name, Find: "<key>CFBundleGetInfoString</key>", Offset: 1,
				Format: "\t\t<string>%s</string>"},
			{Value: platform.Identifier, Find: "<key>CFBundleIdentifier</key>", Offset: 1,
				Format: "\t\t<string>%s</string>"},
			{Value: platform.Name, Find: "<key>CFBundleName</key>", Offset: 1,
				Format: "\t\t<string>%s</string>"},
			{Value: numeric, Find: "<key>CFBundleShortVersionString</key>", Offset: 1,
				Format: "\t\t<string>%s</string>", Version: true},
			{Value: platform.Identifier, Find: "<key>CFBundleURLName</key>", Offset: 1,
				Format: "\t\t\t\t<string>%s</string>"},
			{Value: platform.Scheme, Find: "<key>CFBundleURLSchemes</key>", Offset: 2,
				Format: "\t\t\t\t\t<string>%s</string>"},
			{Value: numeric, Find: "<key>CFBundleVersion</key>", Offset: 1,
				Format: "\t\t<string>%s</string>", Version: true},
			{Value: platform.Copyright, Find: "<key>NSHumanReadableCopyright</key>", Offset: 1,
				Format: "\t\t<string>%s</string>"},
		}}
	}

	return []File{
		{
			Path: filepath.Join(root, "build/config.yml"),
			Fields: []Field{
				{Value: platform.Company, Find: `companyName: "`, Format: `  companyName: "%s"`},
				{Value: platform.Name, Find: `productName: "`, Format: `  productName: "%s"`},
				{Value: platform.Identifier, Find: `productIdentifier: "`,
					Format: `  productIdentifier: "%s"`},
				{Value: platform.Description, Find: `description: "`,
					Format: `  description: "%s"`},
				{Value: platform.Copyright, Find: `copyright: "`, Format: `  copyright: "%s"`},
				{Value: platform.Name, Find: `comments: "`, Format: `  comments: "%s"`},
				{Value: version, Find: `version: "`, Format: `  version: "%s"`, Version: true},
				{Value: platform.Scheme, Find: "- scheme: ", Format: "  - scheme: %s"},
				{Value: platform.Name + " Protocol", Find: "    description: ",
					Format: "    description: %s"},
			},
		},
		plist("build/darwin/Info.plist"),
		plist("build/darwin/Info.dev.plist"),
		{
			Path: filepath.Join(root, "build/windows/info.json"),
			Fields: []Field{
				{Value: numeric, Find: `"file_version": "`, Format: "\t\t\"file_version\": \"%s\",",
					Version: true},
				{Value: numeric, Find: `"product_version": "`, Format: "\t\t\"product_version\": \"%s\"",
					Version: true},
				{Value: version, Find: `"FileVersion": "`,
					Format: "\t\t\t\"FileVersion\": \"%s\",", Version: true},
				{Value: version, Find: `"ProductVersion": "`,
					Format: "\t\t\t\"ProductVersion\": \"%s\",", Version: true},
				{Value: platform.Company, Find: `"CompanyName": "`,
					Format: "\t\t\t\"CompanyName\": \"%s\","},
				{Value: platform.Description, Find: `"FileDescription": "`,
					Format: "\t\t\t\"FileDescription\": \"%s\","},
				{Value: platform.Copyright, Find: `"LegalCopyright": "`,
					Format: "\t\t\t\"LegalCopyright\": \"%s\","},
				{Value: platform.Name, Find: `"ProductName": "`,
					Format: "\t\t\t\"ProductName\": \"%s\","},
				{Value: platform.Name, Find: `"Comments": "`,
					Format: "\t\t\t\"Comments\": \"%s\""},
			},
		},
		{
			Path: filepath.Join(root, "build/windows/nsis/project.nsi"),
			Fields: []Field{
				{Value: numeric + ".0", Find: `!define INFO_FILEVERSION "`,
					Format: `!define INFO_FILEVERSION "%s"`, Version: true},
			},
		},
		{
			Path: filepath.Join(root, "build/windows/nsis/wails_tools.nsh"),
			Fields: []Field{
				{Value: platform.Slug, Find: `!define INFO_PROJECTNAME "`, Format: `    !define INFO_PROJECTNAME "%s"`},
				{Value: platform.Company, Find: `!define INFO_COMPANYNAME "`, Format: `    !define INFO_COMPANYNAME "%s"`},
				{Value: platform.Name, Find: `!define INFO_PRODUCTNAME "`, Format: `    !define INFO_PRODUCTNAME "%s"`},
				{Value: version, Find: `!define INFO_PRODUCTVERSION "`, Format: `    !define INFO_PRODUCTVERSION "%s"`, Version: true},
				{Value: platform.Copyright, Find: `!define INFO_COPYRIGHT "`, Format: `    !define INFO_COPYRIGHT "%s"`},
			},
		},
		{
			Path: filepath.Join(root, "build/linux/nfpm/nfpm.yaml"),
			Fields: []Field{
				{Value: platform.Slug, Find: `name: "`, Format: `name: "%s"`},
				{Value: version, Find: `version: "`, Format: `version: "%s"`, Version: true},
				{Value: platform.Description, Find: "description: |-", Offset: 1, Format: "  %s"},
				{Value: platform.Company, Find: `vendor: "`, Format: `vendor: "%s"`},
				{Value: platform.Homepage(), Find: `homepage: "`, Format: `homepage: "%s"`},
			},
		},
		{
			Path: filepath.Join(root, "build/windows/wails.exe.manifest"),
			Fields: []Field{
				{
					Find:    `processorArchitecture="*"/>`,
					Version: true,
					Render: func() string {
						return fmt.Sprintf(
							"    <assemblyIdentity type=\"win32\" name=\"%s\" version=\"%s\" "+
								"processorArchitecture=\"*\"/>",
							platform.Identifier, numeric+".0")
					},
				},
			},
		},
	}
}

func locate(lines []string, field Field) (int, error) {
	var hits []int
	for i, line := range lines {
		if strings.Contains(line, field.Find) {
			hits = append(hits, i)
		}
	}

	switch len(hits) {
	case 0:
		// The usual cause is a regenerated file: the packagers' templates come from the framework, and
		// one of them coming back in its original shape is not an error to work around but news.
		return 0, fmt.Errorf("nothing matches %q — was the file regenerated", field.Find)
	case 1:
	default:
		return 0, fmt.Errorf("%q matches %d lines, expected one", field.Find, len(hits))
	}

	i := hits[0] + field.Offset
	if i >= len(lines) {
		return 0, fmt.Errorf("%q is %d lines from the end, so its value is not there",
			field.Find, field.Offset)
	}
	return i, nil
}
