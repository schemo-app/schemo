package domain

type HTTPVersion string

const (
	HTTPAuto HTTPVersion = "auto"
	HTTP1    HTTPVersion = "http1"
)

func (v HTTPVersion) Valid() bool {
	return v == HTTPAuto || v == HTTP1
}

type Requests struct {
	HTTPVersion     HTTPVersion `json:"httpVersion"`
	UserAgent       string      `json:"userAgent"`
	Timeout         int         `json:"timeout"`
	MaxRedirects    int         `json:"maxRedirects"`
	FollowRedirects bool        `json:"followRedirects"`
	Cookies         bool        `json:"cookies"`
	AcceptEncoding  bool        `json:"acceptEncoding"`
	ShowAuto        bool        `json:"showAuto"`
}

func DefaultRequests() Requests {
	return Requests{
		Timeout:         30,
		FollowRedirects: true,
		MaxRedirects:    10,
		Cookies:         true,
		HTTPVersion:     HTTPAuto,
		AcceptEncoding:  true,
	}
}

func (r Requests) Valid() error {
	if !r.HTTPVersion.Valid() {
		return Refuse(CodeUnknownHTTPVersion, ErrNotAllowed, Args{"version": string(r.HTTPVersion)})
	}
	return nil
}

type Network struct {
	Proxy        ProxySettings `json:"proxy"`
	Certificates Certificates  `json:"certificates"`
	Requests     Requests      `json:"requests"`
}
