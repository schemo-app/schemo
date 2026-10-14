package draft

import "schemo/internal/domain"

// TextField names one of the two texts the window owns while they are being typed in. Everything
// else about the draft is edited a row at a time, by id.
type TextField string

const (
	FieldURL  TextField = "url"
	FieldBody TextField = "body"
)

// Prepared is the draft ready to go out: its variables filled in, and beside it the copy that
// outlives the moment of sending — a preview, an export, the record. Only this side ever sees the
// first one.
type Prepared struct {
	Method  string
	URL     string
	Headers []domain.HeaderPair
	Body    string
	// The format and what the body was rendered from travel beside the text: a masked copy of a form
	// or of a file cannot be made from the text, so whoever masks this request again needs these.
	BodyKind domain.BodyKind
	Form     []domain.FormRow
	BodyFile string

	MaskedURL     string
	MaskedHeaders []domain.HeaderPair
	MaskedBody    string

	// Digest is a credential the request cannot carry: it goes to the engine, which is the only side
	// that can be there when the server says how. Nothing above the engine has a header to show for
	// it, which is why it travels beside them rather than among them.
	Digest *domain.DigestCredentials
}
