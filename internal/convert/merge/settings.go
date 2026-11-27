package merge

import (
	"slices"
	"strings"

	"schemo/internal/domain"
)

// settings merges what the collection and its folders answer for, and reports whether the person
// takes it. The name given when linking is the person's; a later rename of the API follows.
func (r *run) settings() bool {
	item := &Item{ID: "settings", Name: r.in.Mine.Name, Selected: true}
	mine, theirs := r.in.Mine, r.in.Theirs
	var was *Level
	if r.in.Base != nil {
		was = r.in.Base.Root
	}
	var past Level
	if was != nil {
		past = *was
	}
	before := func(text string) *string {
		if was == nil {
			return nil
		}
		return &text
	}

	patch := patchOf(mine)
	if was != nil && r.weigh(item, Change{Field: FieldName, Mine: mine.Name, Theirs: theirs.Name}, "",
		&past.Name, mine.Name, theirs.Name) {
		patch.Name = theirs.Name
	}
	if r.weigh(item, Change{Field: FieldDescription, Mine: shorten(mine.Description), Theirs: shorten(theirs.Description)},
		"", before(strings.TrimSpace(past.Description)),
		strings.TrimSpace(mine.Description), strings.TrimSpace(theirs.Description)) {
		patch.Description = theirs.Description
	}
	if r.weigh(item, Change{Field: FieldAuth, Mine: authShown(mine.Auth), Theirs: authShown(theirs.Auth)},
		"", before(authForm(past.Auth)), authForm(mine.Auth), authForm(theirs.Auth)) {
		patch.Auth = theirs.Auth
	}
	patch.Variables = mergeList(r, item, variableList, past.Variables, was != nil, mine.Variables, theirs.Variables)
	r.patch(patch, mine)
	r.folderSettings(item)

	if len(item.Changes) > 0 {
		r.review.Settings = item
	}
	applied := len(item.Changes) == 0 || r.selected(*item)
	r.result.Snapshot.Root = was
	if applied {
		r.result.Snapshot.Root = &Level{
			Name: theirs.Name, Description: theirs.Description, Auth: theirs.Auth, Variables: theirs.Variables,
		}
	}
	return applied
}

// folderSettings merges each matched folder's description, and the name of a renamed one.
func (r *run) folderSettings(item *Item) {
	recorded := r.recordedFolders()
	for _, theirs := range r.theirs.folders[1:] {
		key := pathKey(theirs.path)
		held := r.folders.saved[r.folders.ids[key]]
		if held == nil {
			continue
		}
		mine := held.collection
		patch := patchOf(mine)
		label := strings.Join(theirs.path, " / ")
		was, known := recorded[key]
		if renamed, ok := r.folders.renamed[key]; ok {
			was, known = renamed, true
			name := renamed.Path[len(renamed.Path)-1]
			change := Change{
				Field: FieldFolder, Name: label, Attribute: AttributeName, Mine: mine.Name, Theirs: theirs.collection.Name,
			}
			if r.weigh(item, change, key, &name, mine.Name, theirs.collection.Name) {
				patch.Name = theirs.collection.Name
			}
		}
		var past *string
		if known {
			text := strings.TrimSpace(was.Description)
			past = &text
		}
		change := Change{
			Field: FieldFolder, Name: label, Attribute: AttributeDescription,
			Mine: shorten(mine.Description), Theirs: shorten(theirs.collection.Description),
		}
		if r.weigh(item, change, key, past, strings.TrimSpace(mine.Description),
			strings.TrimSpace(theirs.collection.Description)) {
			patch.Description = theirs.collection.Description
		}
		r.patch(patch, mine)
	}
}

// patch writes a level's new answers, where they differ from what it holds.
func (r *run) patch(patch domain.LevelPatch, level domain.Collection) {
	if patch.Name != level.Name || patch.Description != level.Description ||
		!same(patch.Auth, level.Auth) || !sameRows(patch.Variables, level.Variables) {
		r.result.Update.Patches = append(r.result.Update.Patches, patch)
	}
}

func patchOf(level domain.Collection) domain.LevelPatch {
	return domain.LevelPatch{
		ID: level.ID, Name: level.Name, Description: level.Description,
		Auth: level.Auth, Variables: level.Variables,
	}
}

// environments merges the values the description gives the collection in each environment. It
// removes neither a value nor an environment, and leaves a read-only one alone.
func (r *run) environments() {
	recorded := map[string]domain.EnvironmentDraft{}
	if r.in.Base != nil {
		for _, draft := range r.in.Base.Environments {
			recorded[environmentKey(draft.Name)] = draft
		}
	}
	for _, draft := range r.in.Environments {
		name, values := strings.TrimSpace(draft.Name), blockOf(draft)
		if name == "" || len(values) == 0 {
			continue
		}
		was, known := recorded[environmentKey(name)]
		remember := func(one domain.EnvironmentDraft, ok bool) {
			if ok {
				r.result.Snapshot.Environments = append(r.result.Snapshot.Environments, one)
			}
		}
		at := slices.IndexFunc(r.in.Held, func(one domain.Environment) bool {
			return strings.EqualFold(strings.TrimSpace(one.Name), name)
		})
		var held []domain.Variable
		item := &Item{ID: "env:" + environmentKey(name), Name: name, Selected: true}
		if at >= 0 {
			environment := r.in.Held[at]
			if environment.Readonly {
				remember(was, known)
				continue
			}
			item.Name, held = environment.Name, environment.Block(r.in.Block)
		}
		var past []domain.Variable
		if known {
			past = blockOf(was)
		}
		merged := mergeList(r, item, variableList, past, known, held, values)
		if len(item.Changes) > 0 {
			r.review.Environments = append(r.review.Environments, *item)
		}
		if len(item.Changes) > 0 && !r.selected(*item) {
			remember(was, known)
			continue
		}
		if r.choices != nil && !r.linking && !sameRows(merged, held) {
			r.result.Environments = append(r.result.Environments, domain.EnvironmentDraft{
				Name: item.Name, Color: draft.Color, Blocks: []domain.BlockDraft{{Variables: merged}},
			})
		}
		remember(draft, true)
	}
}

func environmentKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

func blockOf(draft domain.EnvironmentDraft) []domain.Variable {
	var out []domain.Variable
	for _, block := range draft.Blocks {
		if block.Collection == 0 {
			out = append(out, block.Variables...)
		}
	}
	return out
}

// authForm is an authorization as compared: none of its own is "inherit", and a field left at the
// scheme's default says nothing.
func authForm(auth *domain.Auth) string {
	if auth == nil || auth.Type == "" || auth.Type == domain.AuthInherit {
		return string(domain.AuthInherit)
	}
	return string(auth.Type) + " " + authFields(*auth, false)
}

// authShown is an authorization as the review draws it, secrets written out only as references.
func authShown(auth *domain.Auth) string {
	if auth == nil || auth.Type == "" || auth.Type == domain.AuthInherit {
		return string(domain.AuthInherit)
	}
	if fields := authFields(*auth, true); fields != "" {
		return string(auth.Type) + " · " + fields
	}
	return string(auth.Type)
}

// authFields writes the fields of the scheme in use that differ from its defaults. What another
// scheme left stored is not sent, and only this scheme says which values are secret.
func authFields(auth domain.Auth, masked bool) string {
	scheme, _ := domain.SchemeFor(auth.Type)
	var out []string
	for _, field := range scheme.Fields {
		value, set := auth.Fields[field.Key]
		if !set || value == field.Default {
			continue
		}
		reference := strings.HasPrefix(value, "{{") && strings.HasSuffix(value, "}}")
		if masked && field.Secret && value != "" && !reference {
			value = domain.SecretMask
		}
		out = append(out, field.Key+"="+value)
	}
	return strings.Join(out, ", ")
}
