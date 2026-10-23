package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/schemo-app/proto/secret"

	"schemo/internal/domain"
)

// A row travels as it is stored: its data JSON, beside the paths in it that hold a secret. The
// registry below is everything the sync engine knows about a kind kept in the entities table; the
// queue, the push, the pull and the conflicts are written once for all of them.

// syncKind is one kind that travels.
type syncKind struct {
	// parent is the kind a row's parent id points at, and parentRequired says a row without one is
	// malformed. A row whose parent is not here yet is retried after the rest of the page.
	parent         domain.EntityType
	parentRequired bool
	// secrets names the slots of a row's data that hold a secret; nil for a kind with none.
	secrets func(row entity) ([]string, error)
	// key is the natural key a received row is filed under; nil for a kind keyed by id alone.
	key func(row entity) string
	// label is how the queue names a row to a person; nil reads the row's name.
	label func(row entity) string
}

var syncKinds = map[domain.EntityType]syncKind{
	domain.EntityCollection: {
		parent: domain.EntityCollection,
		secrets: func(row entity) ([]string, error) {
			c, err := decodeData[domain.Collection](row)
			if err != nil {
				return nil, err
			}
			out := authSecrets(c.Auth)
			for _, v := range c.Variables {
				if v.Kind == domain.VariableSecret {
					out = append(out, "/variables/@"+secret.Escape(v.ID)+"/value")
				}
			}
			return out, nil
		},
	},
	domain.EntityRequest: {
		parent: domain.EntityCollection, parentRequired: true,
		secrets: func(row entity) ([]string, error) {
			n, err := decodeData[domain.CollectionNode](row)
			if err != nil {
				return nil, err
			}
			return authSecrets(n.Auth), nil
		},
	},
	domain.EntityEnvironment: {},
	domain.EntityVariable: {
		parent: domain.EntityEnvironment,
		secrets: func(row entity) ([]string, error) {
			v, err := decodeData[domain.Variable](row)
			if err != nil || v.Kind != domain.VariableSecret {
				return nil, err
			}
			return []string{"/value"}, nil
		},
		key: func(row entity) string { return naturalKey(row.Parent, row.Name) },
	},
	domain.EntityJarDomain: {
		key: func(row entity) string { return naturalKey(row.Name) },
	},
	domain.EntityCookie: {
		secrets: func(entity) ([]string, error) { return []string{"/value"}, nil },
		key: func(row entity) string {
			var c struct {
				Path string `json:"path"`
			}
			_ = json.Unmarshal(row.Data, &c)
			if c.Path == "" {
				c.Path = "/"
			}
			return naturalKey(row.Parent, c.Path, row.Name)
		},
		label: func(row entity) string { return row.Name + " · " + row.Parent },
	},
}

// authSecrets names the auth fields the scheme marks secret, among those the auth has.
func authSecrets(auth *domain.Auth) []string {
	if auth == nil {
		return nil
	}
	scheme, ok := domain.SchemeFor(auth.Type)
	if !ok {
		return nil
	}
	var out []string
	for _, field := range scheme.Fields {
		if _, held := auth.Fields[field.Key]; field.Secret && held {
			out = append(out, "/auth/fields/"+secret.Escape(field.Key))
		}
	}
	return out
}

// keyOf is the natural key a received row is filed under.
func keyOf(row entity) string {
	if spec := syncKinds[row.Kind]; spec.key != nil {
		return spec.key(row)
	}
	return ""
}

// heldData is what this device holds of a row the server sent with secrets withheld: the row
// itself when it is here, else the live row with the same natural key.
func heldData(ctx context.Context, tx *writeTx, e entity) []byte {
	if row, err := anyEntity(ctx, tx, e.ID); err == nil {
		return row.Data
	}
	if e.Key == "" {
		return nil
	}
	rows, err := listEntities(ctx, tx, e.Workspace, kinds(e.Kind), `key = ?`, e.Key)
	if err != nil || len(rows) == 0 {
		return nil
	}
	return rows[0].Data
}

// outgoing is a row's data as it leaves: with its secrets, or without them when they stay here.
func outgoing(row entity, scopes domain.SyncScopes) (data []byte, secrets, withheld []string, err error) {
	if spec := syncKinds[row.Kind]; spec.secrets != nil {
		if secrets, err = spec.secrets(row); err != nil {
			return nil, nil, nil, err
		}
	}
	data = row.Data
	if !scopes.Secrets && len(secrets) > 0 {
		data, withheld, err = secret.Withhold(data, secrets)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("withholding the secrets of %s %s: %w", row.Kind, row.ID, err)
		}
	}
	return data, secrets, withheld, nil
}
