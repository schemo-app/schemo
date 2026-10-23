package command_test

import (
	"testing"

	"schemo/internal/convert/command"
	"schemo/internal/domain"
)

// An upload is a body a notation writes as a reference to a file; which notations can is the tool's
// business. The seeds are shaped the way a copy leaves them.
func TestAUploadIsWrittenWhereTheNotationCan(t *testing.T) {
	binary := command.Request{
		Method:   "POST",
		URL:      "https://api.example.com/a",
		BodyKind: domain.BodyBinary,
		BodyFile: "/tmp/logo.png",
	}
	form := command.Request{
		Method:   "POST",
		URL:      "https://api.example.com/a",
		BodyKind: domain.BodyForm,
		Form: []domain.FormRow{
			{Name: "note", Value: "hello", Enabled: true},
			{Name: "logo", Src: "/tmp/logo.png", File: true, Enabled: true},
		},
	}

	cases := []struct {
		name    string
		format  command.Format
		seed    command.Request
		want    string
		omitted []command.Omission
	}{
		{
			name:   "curl writes a whole body as a file it reads",
			format: command.FormatCurl,
			seed:   binary,
			want:   "curl -X POST --data-binary @'/tmp/logo.png' 'https://api.example.com/a'",
		},
		{
			name:   "curl writes the fields of a form, the file among them",
			format: command.FormatCurl,
			seed:   form,
			want: "curl -X POST -F 'note=hello' -F 'logo=@/tmp/logo.png' " +
				"'https://api.example.com/a'",
		},
		{
			name:   "httpie writes the fields of a form, the file among them",
			format: command.FormatHTTPie,
			seed:   form,
			want:   "http --form POST 'https://api.example.com/a' 'note=hello' 'logo@/tmp/logo.png'",
		},
		{
			name:   "PowerShell passes a whole body as a file",
			format: command.FormatPowerShell,
			seed:   binary,
			want: "Invoke-RestMethod -Method POST -Uri 'https://api.example.com/a' " +
				"-InFile '/tmp/logo.png'",
		},
		{
			name:    "wget leaves a whole body out and names the file",
			format:  command.FormatWget,
			seed:    binary,
			want:    "wget --method=POST 'https://api.example.com/a'",
			omitted: []command.Omission{{Kind: command.OmissionUpload, Name: "logo.png"}},
		},
		{
			name:    "fetch leaves a whole body out and names the file",
			format:  command.FormatFetch,
			seed:    binary,
			want:    "fetch('https://api.example.com/a', {\"method\":\"POST\"})",
			omitted: []command.Omission{{Kind: command.OmissionUpload, Name: "logo.png"}},
		},
		{
			name:    "httpie leaves a body that is a whole file out",
			format:  command.FormatHTTPie,
			seed:    binary,
			want:    "http POST 'https://api.example.com/a'",
			omitted: []command.Omission{{Kind: command.OmissionUpload, Name: "logo.png"}},
		},
		{
			name:    "PowerShell leaves the fields of a form out",
			format:  command.FormatPowerShell,
			seed:    form,
			want:    "Invoke-RestMethod -Method POST -Uri 'https://api.example.com/a'",
			omitted: []command.Omission{{Kind: command.OmissionUpload, Name: "logo"}},
		},
		{
			name:    "wget leaves the fields of a form out, naming the file among them",
			format:  command.FormatWget,
			seed:    form,
			want:    "wget --method=POST 'https://api.example.com/a'",
			omitted: []command.Omission{{Kind: command.OmissionUpload, Name: "logo"}},
		},
		{
			name:    "fetch leaves the fields of a form out, naming the file among them",
			format:  command.FormatFetch,
			seed:    form,
			want:    "fetch('https://api.example.com/a', {\"method\":\"POST\"})",
			omitted: []command.Omission{{Kind: command.OmissionUpload, Name: "logo"}},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, omitted := command.Render(c.format, c.seed)
			if got != c.want {
				t.Errorf("Render = %q, want %q", got, c.want)
			}
			if len(omitted) != len(c.omitted) {
				t.Fatalf("omitted = %+v, want %+v", omitted, c.omitted)
			}
			for i := range omitted {
				if omitted[i] != c.omitted[i] {
					t.Errorf("omission %d = %+v, want %+v", i, omitted[i], c.omitted[i])
				}
			}
		})
	}
}

// A body whose file was never chosen is not an upload: there is nothing to write and nothing to
// report, which is what an empty path means everywhere else in the draft too.
func TestABodyWithNoFileChosenWritesNothing(t *testing.T) {
	got, omitted := command.Render(command.FormatCurl, command.Request{
		Method:   "POST",
		URL:      "https://api.example.com/a",
		BodyKind: domain.BodyBinary,
	})
	if got != "curl -X POST 'https://api.example.com/a'" {
		t.Errorf("Render = %q, want the request without a body", got)
	}
	if len(omitted) != 0 {
		t.Errorf("omitted = %+v, want nothing: there was no file to leave out", omitted)
	}
}
