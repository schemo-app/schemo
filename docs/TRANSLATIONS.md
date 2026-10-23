# Translations

Translations live in [frontend/src/i18n/locales](../frontend/src/i18n/locales).

[en.json](../frontend/src/i18n/locales/en.json) is the reference catalogue.

No Go or TypeScript changes are required to add a language.

## Fix a Translation

Edit the corresponding locale file and open a pull request against `main`.

Small fixes can be made directly through GitHub's file editor.

CI validates translation files automatically.

## Add a Language

1. Check open pull requests to avoid duplicate work.
2. Copy `../frontend/src/i18n/locales/en.json`.
3. Name the file with a canonical BCP 47 tag, for example:

```text
de.json
pt-BR.json
```

4. Add the language to [frontend/src/i18n/languages.json](../frontend/src/i18n/languages.json):

```json
"de": "Deutsch"
```

5. Translate the values and open a pull request.

The language appears automatically in Settings and secondary windows.

Keep all keys. Leave the English value when a translation is still missing.

## Message Format

- Preserve keys and nesting.
- Preserve parameters such as `{name}`, `{n}` and `{error}`. Braces mean nothing else.
- Keep technical identifiers, URLs, shortcuts and units accurate.
- Use concise wording that fits buttons and menus.
- JSON uses double quotes, no comments and no trailing commas.

Plural forms use ` | `; `|` means nothing else:

```json
"requests": "{n} request | {n} requests"
```

A message is plural in your language exactly when it is plural in English. Plural selection uses
`Intl.PluralRules`; no language-specific code is required.

Two forms mean `one | other`. For more forms, use the supported categories in this order:
`zero | one | two | few | many | other`, omitting categories your language does not use.
Extra categories use the last supplied form; Russian currently uses `one | few | many/other`.

## Check Your Changes

Use the Node.js version from [.node-version](../.node-version):

```sh
cd frontend
npm ci
npm run translations:check
```

The checker validates:

- missing, unknown and empty keys;
- stray braces;
- plural forms;
- parameters.

To preview the translation:

```sh
task dev
```

Then select the language in Settings.
