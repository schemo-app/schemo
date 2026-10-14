package domain

type Certificates struct {
	Verify         bool   `json:"verify"`
	TrustLocalhost bool   `json:"trustLocalhost"`
	Roots          []Root `json:"roots"`
}

// Root is one authority somebody added, with the name it is shown under.
type Root struct {
	Name string `json:"name"`
	PEM  string `json:"pem"`
}

// DefaultCertificates is what the app checks before anybody says otherwise.
func DefaultCertificates() Certificates {
	return Certificates{Verify: true, TrustLocalhost: true}
}
