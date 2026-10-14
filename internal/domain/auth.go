package domain

type AuthType string

const (
	AuthNone    AuthType = "none"
	AuthInherit AuthType = "inherit"
	AuthBearer  AuthType = "bearer"
	AuthBasic   AuthType = "basic"
	AuthAPIKey  AuthType = "apikey"
	AuthOAuth2  AuthType = "oauth2"
	AuthJWT     AuthType = "jwt"
	AuthDigest  AuthType = "digest"
	AuthAWS     AuthType = "aws"
)

const (
	PlaceHeader = "header"
	PlaceQuery  = "query"
)

type Auth struct {
	Type   AuthType          `json:"type"`
	Fields map[string]string `json:"fields,omitempty"`
}

func NewAuth(t AuthType) Auth {
	return Auth{Type: t, Fields: map[string]string{}}
}

func (a Auth) Answer(key string) string { return a.Fields[key] }

func (a Auth) OrDefault(key string) string {
	if value, given := a.Fields[key]; given {
		return value
	}
	scheme, ok := SchemeFor(a.Type)
	if !ok {
		return ""
	}
	for _, field := range scheme.Fields {
		if field.Key == key {
			return field.Default
		}
	}
	return ""
}

func (a *Auth) Answered() bool {
	return a != nil && a.Type != AuthNone && a.Type != AuthInherit && a.Type != ""
}

func (a Auth) Stored() *Auth {
	if a.Type == AuthInherit {
		// «Inherit» is chosen rather than reached: a tree that dropped it would draw the level as
		// «None» — the one answer it did not give — and the choice would be gone on reopen.
		return &Auth{Type: AuthInherit}
	}
	if !a.Answered() && len(a.Fields) == 0 {
		return nil
	}
	return &a
}

func (a Auth) Normalized() Auth {
	scheme, ok := SchemeFor(a.Type)
	if !ok {
		return NewAuth(a.Type)
	}
	out := make(map[string]string, len(a.Fields)+len(scheme.Fields))
	for key, value := range a.Fields {
		out[key] = value
	}
	for _, field := range scheme.Fields {
		if _, given := out[field.Key]; !given && field.Default != "" {
			out[field.Key] = field.Default
		}
	}
	return Auth{Type: a.Type, Fields: out}
}

func (a Auth) With(key, value string) Auth {
	fields := make(map[string]string, len(a.Fields)+1)
	for k, v := range a.Fields {
		fields[k] = v
	}
	fields[key] = value
	return Auth{Type: a.Type, Fields: fields}
}

type FieldKind string

const (
	FieldText     FieldKind = "text"
	FieldPassword FieldKind = "password"
	FieldTextarea FieldKind = "textarea"
	FieldNumber   FieldKind = "number"
	FieldSelect   FieldKind = "select"
)

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Field struct {
	Key         string     `json:"key"`
	Kind        FieldKind  `json:"kind"`
	Label       string     `json:"label"`
	Placeholder string     `json:"placeholder,omitempty"`
	Hint        string     `json:"hint,omitempty"`
	Options     []Option   `json:"options,omitempty"`
	Default     string     `json:"default,omitempty"`
	Secret      bool       `json:"secret,omitempty"`
	Full        bool       `json:"full,omitempty"`
	Mono        bool       `json:"mono,omitempty"`
	Height      int        `json:"height,omitempty"`
	When        *Condition `json:"when,omitempty"`
}

type Condition struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
}

type Scheme struct {
	Type        AuthType `json:"type"`
	Label       string   `json:"label"`
	Menu        string   `json:"menu,omitempty"`
	Where       string   `json:"where,omitempty"`
	Sends       string   `json:"sends,omitempty"`
	Note        string   `json:"note,omitempty"`
	Fields      []Field  `json:"fields"`
	Primary     bool     `json:"primary"`
	NeedsParent bool     `json:"needsParent,omitempty"`
	Fetches     bool     `json:"fetches,omitempty"`
}

// grants is the condition "only for these grants": OAuth 2 asks different questions of each of its
// four, and the fields that belong to some of them stand behind the choice of grant.
func grants(values ...string) *Condition {
	return &Condition{Key: "grant", Values: values}
}

// usernameAndPassword is the pair Basic and Digest are both asked for: the same two words for two
// schemes that differ in what they do with them.
func usernameAndPassword() []Field {
	return []Field{
		{Key: "username", Kind: FieldText, Label: "request.auth.field.username",
			Placeholder: "request.auth.ph.username"},
		{Key: "password", Kind: FieldPassword, Label: "request.auth.field.password",
			Placeholder: "request.auth.ph.password", Secret: true},
	}
}

// tokenPlacement is where a token that was obtained or signed is put on the request, and what it is
// called there. OAuth 2 and JWT end on the same two fields.
func tokenPlacement() []Field {
	return []Field{
		{Key: "place", Kind: FieldSelect, Label: "request.auth.field.addTokenTo",
			Default: "header", Full: true, Options: []Option{
				{Value: "header", Label: "request.auth.place.header"},
				{Value: "query", Label: "request.auth.place.query"},
			}},
		{Key: "prefix", Kind: FieldText, Label: "request.auth.field.headerPrefix",
			Placeholder: "request.auth.ph.bearer", Default: "Bearer", Full: true},
	}
}

// The OAuth 2.0 grants, named as the protocol writes them: four conversations with one endpoint.
const (
	GrantClientCredentials = "client_credentials"
	GrantAuthorizationCode = "authorization_code"
	GrantImplicit          = "implicit"
	GrantPassword          = "password"
)

// GrantNamed is the grant a file names, and the authorization code flow where it names none or one
// this app does not run: the flow an OAuth 2.0 scheme without a grant is read as.
func GrantNamed(name string) string {
	switch name {
	case GrantImplicit, GrantPassword, GrantClientCredentials:
		return name
	default:
		return GrantAuthorizationCode
	}
}

var authSchemes = []Scheme{
	{
		Type: AuthNone, Primary: true, Label: "request.auth.none", Menu: "request.auth.noneMenu",
		Sends: "request.auth.sends.none",
	},
	{
		Type: AuthInherit, Primary: true, NeedsParent: true, Label: "request.auth.inheritShort",
		Menu: "request.auth.inheritMenu", Where: "request.auth.where.parent",
		Sends: "request.auth.sends.inherit",
	},
	{
		Type: AuthBearer, Primary: true, Label: "request.auth.bearer", Menu: "request.auth.bearerMenu",
		Where: "request.auth.where.header", Sends: "request.auth.sends.bearer",
		Fields: []Field{
			{Key: "prefix", Kind: FieldText, Label: "request.auth.field.prefix",
				Placeholder: "request.auth.ph.bearer", Default: "Bearer", Full: true},
			{Key: "token", Kind: FieldTextarea, Label: "request.auth.field.token",
				Placeholder: "request.auth.ph.token", Secret: true, Full: true, Height: 64},
		},
	},
	{
		Type: AuthBasic, Primary: true, Label: "request.auth.basic", Menu: "request.auth.basicMenu",
		Where: "request.auth.where.header", Sends: "request.auth.sends.basic",
		Fields: usernameAndPassword(),
	},
	{
		Type: AuthAPIKey, Primary: true, Label: "request.auth.apikey",
		Where: "request.auth.where.headerQuery", Sends: "request.auth.sends.apikey",
		Fields: []Field{
			{Key: "key", Kind: FieldText, Label: "request.auth.field.key",
				Placeholder: "request.auth.ph.apiKey", Mono: true},
			{Key: "value", Kind: FieldText, Label: "request.auth.field.value",
				Placeholder: "request.auth.ph.apiKeyValue", Secret: true},
			{Key: "place", Kind: FieldSelect, Label: "request.auth.field.addTo",
				Default: "header", Full: true, Options: []Option{
					{Value: "header", Label: "request.auth.place.header"},
					{Value: "query", Label: "request.auth.place.query"},
				}},
		},
	},
	{
		Type: AuthOAuth2, Label: "request.auth.oauth2Short", Menu: "request.auth.oauth2", Fetches: true,
		Where: "request.auth.where.tokenUrl", Sends: "request.auth.sends.oauth2",
		Fields: append([]Field{
			{Key: "grant", Kind: FieldSelect, Label: "request.auth.field.grantType",
				Default: GrantClientCredentials, Full: true, Options: []Option{
					{Value: GrantClientCredentials, Label: "request.auth.grant.clientCredentials"},
					{Value: GrantAuthorizationCode, Label: "request.auth.grant.authorizationCode"},
					{Value: GrantImplicit, Label: "request.auth.grant.implicit"},
					{Value: GrantPassword, Label: "request.auth.grant.password"},
				}},
			// Where the person is sent to say yes. Only the two grants that send them anywhere have it.
			{Key: "authUrl", Kind: FieldText, Label: "request.auth.field.authUrl",
				Placeholder: "request.auth.ph.authUrl", Mono: true, Full: true,
				When: grants(GrantAuthorizationCode, GrantImplicit)},
			// And where a token is exchanged for; the implicit grant is given one outright.
			{Key: "tokenUrl", Kind: FieldText, Label: "request.auth.field.tokenUrl",
				Placeholder: "request.auth.ph.tokenUrl", Mono: true, Full: true,
				When: grants(GrantClientCredentials, GrantAuthorizationCode, GrantPassword)},
			{Key: "clientId", Kind: FieldText, Label: "request.auth.field.clientId",
				Placeholder: "request.auth.ph.clientId", Full: true},
			{Key: "clientSecret", Kind: FieldPassword, Label: "request.auth.field.clientSecret",
				Placeholder: "request.auth.ph.clientSecret", Secret: true, Full: true,
				When: grants(GrantClientCredentials, GrantAuthorizationCode, GrantPassword)},
			{Key: "clientAuth", Kind: FieldSelect, Label: "request.auth.field.clientAuth",
				Default: "header", Full: true,
				When: grants(GrantClientCredentials, GrantAuthorizationCode, GrantPassword),
				Options: []Option{
					{Value: "header", Label: "request.auth.clientAuth.header"},
					{Value: "body", Label: "request.auth.clientAuth.body"},
				}},
			{Key: "owner", Kind: FieldText, Label: "request.auth.field.owner",
				Placeholder: "request.auth.ph.username", Full: true,
				When: grants("password")},
			{Key: "ownerPassword", Kind: FieldPassword, Label: "request.auth.field.ownerPassword",
				Placeholder: "request.auth.ph.password", Secret: true, Full: true,
				When: grants("password")},
			{Key: "scope", Kind: FieldText, Label: "request.auth.field.scope",
				Placeholder: "request.auth.ph.scope", Mono: true, Full: true},
			{Key: "audience", Kind: FieldText, Label: "request.auth.field.audience",
				Placeholder: "request.auth.ph.audience", Mono: true, Full: true},
		}, tokenPlacement()...),
	},
	{
		Type: AuthJWT, Label: "request.auth.jwt", Menu: "request.auth.jwtMenu",
		Where: "request.auth.where.signed", Sends: "request.auth.sends.jwt",
		Fields: append([]Field{
			{Key: "algorithm", Kind: FieldSelect, Label: "request.auth.field.algorithm",
				Default: "HS256", Full: true, Options: []Option{
					{Value: "HS256", Label: "request.auth.alg.hs256"},
					{Value: "HS384", Label: "request.auth.alg.hs384"},
					{Value: "HS512", Label: "request.auth.alg.hs512"},
					{Value: "RS256", Label: "request.auth.alg.rs256"},
					{Value: "RS384", Label: "request.auth.alg.rs384"},
					{Value: "RS512", Label: "request.auth.alg.rs512"},
				}},
			{Key: "secret", Kind: FieldPassword, Label: "request.auth.field.secret",
				Placeholder: "request.auth.ph.secret", Secret: true, Full: true},
			{Key: "secretEncoding", Kind: FieldSelect, Label: "request.auth.field.secretEncoding",
				Default: "plain", Full: true, Options: []Option{
					{Value: "plain", Label: "request.auth.encoding.plain"},
					{Value: "base64", Label: "request.auth.encoding.base64"},
				}},
			{Key: "payload", Kind: FieldTextarea, Label: "request.auth.field.payload",
				Default: `{ "sub": "1234567890" }`, Hint: "request.auth.hint.payload",
				Full: true, Height: 76},
			// Seconds, as the `exp` claim counts them: an hour is what a token gets when nobody says.
			{Key: "expiresIn", Kind: FieldNumber, Label: "request.auth.field.expiresIn",
				Placeholder: "request.auth.ph.expiresIn", Default: "3600",
				Hint: "request.auth.hint.expiresIn", Mono: true, Full: true},
			{Key: "joseHeader", Kind: FieldTextarea, Label: "request.auth.field.joseHeader",
				Default: `{ "kid": "key-1" }`, Hint: "request.auth.hint.joseHeader",
				Full: true, Height: 56},
		}, tokenPlacement()...),
	},
	{
		Type: AuthDigest, Label: "request.auth.digest", Menu: "request.auth.digestMenu",
		Where: "request.auth.where.challenge", Sends: "request.auth.sends.digest",
		Note:   "request.auth.noteDigest",
		Fields: usernameAndPassword(),
	},
	{
		Type: AuthAWS, Label: "request.auth.aws", Menu: "request.auth.awsMenu",
		Where: "request.auth.where.sigv4", Sends: "request.auth.sends.aws",
		Fields: []Field{
			{Key: "accessKeyId", Kind: FieldText, Label: "request.auth.field.accessKeyId",
				Placeholder: "request.auth.ph.accessKeyId", Mono: true},
			{Key: "secretAccessKey", Kind: FieldPassword, Label: "request.auth.field.secretAccessKey",
				Placeholder: "request.auth.ph.secretAccessKey", Secret: true},
			{Key: "sessionToken", Kind: FieldPassword, Label: "request.auth.field.sessionToken",
				Placeholder: "request.auth.ph.sessionToken", Hint: "request.auth.hint.sessionToken",
				Secret: true, Full: true},
			{Key: "region", Kind: FieldText, Label: "request.auth.field.region",
				Placeholder: "request.auth.ph.region", Mono: true},
			{Key: "service", Kind: FieldText, Label: "request.auth.field.service",
				Placeholder: "request.auth.ph.service", Mono: true},
		},
	},
}

type AuthRequest struct {
	Method  string
	URL     string
	Headers []HeaderPair
	Body    []byte
}

type AuthOutput struct {
	Headers []HeaderPair
	Query   []HeaderPair
	Digest  *DigestCredentials `json:"digest,omitempty"`
}

type AuthToken struct {
	Held      bool  `json:"held"`
	ExpiresAt int64 `json:"expiresAt,omitempty"`
}

type SignInPage struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type SignInPages struct {
	Waiting SignInPage `json:"waiting"`
	Done    SignInPage `json:"done"`
	Refused SignInPage `json:"refused"`
	Failed  string     `json:"failed"`
}

type DigestCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type ProjectedRow struct {
	Target RowKind  `json:"target"`
	Name   string   `json:"name"`
	Value  string   `json:"value"`
	From   AuthType `json:"from"`
}

func (o AuthOutput) Empty() bool { return len(o.Headers) == 0 && len(o.Query) == 0 }

func AuthSchemes() []Scheme { return authSchemes }

func SchemeFor(t AuthType) (Scheme, bool) {
	for _, scheme := range authSchemes {
		if scheme.Type == t {
			return scheme, true
		}
	}
	return Scheme{}, false
}

func FieldKeys(t AuthType) []string {
	scheme, ok := SchemeFor(t)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(scheme.Fields))
	for _, field := range scheme.Fields {
		keys = append(keys, field.Key)
	}
	return keys
}
