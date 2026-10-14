# Translations

Translations live in [frontend/src/locales](../frontend/src/locales).

[en.json](../frontend/src/locales/en.json) is the reference catalogue.

No Go or TypeScript changes are required to add a language.

## Fix a Translation

Edit the corresponding locale file and open a pull request against `main`.

Small fixes can be made directly through GitHub's file editor.

CI validates translation files automatically.

## Add a Language

1. Check open pull requests to avoid duplicate work.
2. Copy `../frontend/src/locales/en.json`.
3. Name the file with a canonical BCP 47 tag, for example:

```text id="0ra9ma"
de.json
pt-BR.json
```

4. Add the language to [frontend/src/languages.json](../frontend/src/languages.json):

```json id="rwv5hf"
"de": "Deutsch"
```

5. Translate the values and open a pull request.

The language appears automatically in Settings and secondary windows.

Keep all keys. Leave the English value when a translation is still missing.

## Message Format

- Preserve keys and nesting.
- Preserve parameters such as `{name}`, `{n}` and `{error}`.
- Preserve linked messages such as `@:common.save`.
- Keep technical identifiers, URLs, shortcuts and units accurate.
- Use concise wording that fits buttons and menus.
- JSON uses double quotes, no comments and no trailing commas.

Escape literal Vue I18n special characters with:

```text id="rxpuzc"
{'@'}
{'{'}
{'}'}
{'|'}
```

Plural forms use ` | `:

```json id="vgyylm"
"requests": "{n} request | {n} requests"
```

Plural selection uses `Intl.PluralRules`; no language-specific code is required.

Two forms mean `one | other`. For more forms, use the supported categories in this order:
`zero | one | two | few | many | other`, omitting categories your language does not use.
Extra categories use the last supplied form; Russian currently uses `one | few | many/other`.

See the [Vue I18n message syntax](https://vue-i18n.intlify.dev/guide/essentials/syntax) for details.

## Check Your Changes

Use the Node.js version from [.node-version](../.node-version):

```sh id="920yt5"
cd frontend
npm ci
npm run translations:check
```

The checker validates:

- missing and unknown keys;
- message syntax;
- linked messages;
- parameters.

To preview the translation:

```sh id="15gybb"
task dev
```

Then select the language in Settings.
