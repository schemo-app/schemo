package desktop

import _ "embed"

// publicKey holds the public release signing key embedded at build time.
//
//go:embed updater.key.pub
var publicKey []byte
