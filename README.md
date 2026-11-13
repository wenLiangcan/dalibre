# Dalibre

A calm, fast desktop library for e-books that reads and writes **calibre
libraries**. Written in Go with [MyGo](https://github.com/egoist/mygo)'s native
UI: no webview, no cgo, one binary.

<img width="3038" height="2176" alt="CleanShot 2026-11-09 at 19 18 34@2x" src="https://github.com/user-attachments/assets/6439498c-3d99-4ac8-b446-7c3b2a39d0c1" />

Design: [Figma — Dalibre](https://www.figma.com/design/tZg6AMrNXoz9VYoa1xZoCo/Dalibre)

## Develop

```sh
go tool mygo dev ./cmd/dalibre        # run with live reload
go test ./...                         # unit + view tests (no window needed)
go run ./cmd/seed "testdata/Sample Library"   # a sample library to play with
```

Point the app at a library with `DALIBRE_LIBRARY` (PowerShell:
`$env:DALIBRE_LIBRARY="C:\path\to\Library"`); otherwise it opens the last one
used, then the one calibre itself uses.

On macOS, mount network shares in Finder, then choose the library folder
on the mounted volume (for example, `/Volumes/Shared/Calibre Library`).
Dalibre uses macOS's system SQLite through purego so Finder volumes use
their native filesystem locking, including SMB and AFP. No cgo is needed.

Mounted-volume regression checks are opt-in: `DALIBRE_REAL_LIBRARY` reads
an existing library without changing it; `DALIBRE_TEST_VOLUME` exercises
writes and competing-writer locks in a new temporary library that it removes.

View tests can save screenshots: `DALIBRE_SHOTS=<folder> go test ./internal/app`.

Build a release (exe + installer in `cmd/dalibre/build`):

```sh
go tool mygo build ./cmd/dalibre
```

### Release

Dalibre updates itself from this repository's GitHub releases. Releases
are signed with the update key of `go tool mygo keygen`, which is the
`MYGO_UPDATER_PRIVATE_KEY` secret of the repository; keep a copy in a
password manager, as installed apps only accept updates it signs.

1. Bump `version` in `cmd/dalibre/mygo.json`, and add its `## <version>`
   section to [CHANGELOG.md](CHANGELOG.md): the update window shows it.
2. Commit, then tag and push: `git tag v0.2.0 && git push origin v0.2.0`.
3. The [Release workflow](.github/workflows/release.yml) builds every
   platform into a draft release. Publish the draft, and installed apps
   update to it.

The workflow signs the macOS app with the Developer ID and notarizes it,
from the `MACOS_CERTIFICATE` and `MACOS_CERTIFICATE_PASSWORD` secrets, and
an App Store Connect API key in `APPLE_API_KEY`, `APPLE_API_KEY_ID` and
`APPLE_API_ISSUER` (see the top of the workflow). `mygo.json`
leaves signing out, so local builds are signed ad hoc.

## Layout

```
cmd/
  dalibre/            the app: main.go, mygo.json, resources/icon.png
  seed/               creates a sample calibre library for development
  dalibre-plugin/     CLI for plugin authors (npm: dalibre-plugin): init, check, dev, docs, pack, install
plugins/
  gutenberg/          Project Gutenberg, built into the app (dalibre.gutenberg)
  googlebooks/        Google Books metadata lookup, built into the app (dalibre.googlebooks)
  neodb/              NeoDB metadata lookup, built into the app (dalibre.neodb)
sdk/                  for plugin authors: dalibre.d.ts, plugin.schema.json, template
internal/
  calibre/            calibre library access: schema, SQL functions its
                      triggers need, naming rules, CRUD, OPF
  ebookmeta/          title/authors/cover from EPUB, MOBI/AZW3 and file names
  catalog/            search, filters, sorting and sidebar counts (no UI)
  plugin/             plugins: manifest, sandboxed JS runtime, consent, packages
  mcp/                the MCP server: tools, call log, stdio bridge
  settings/           user settings persisted as JSON
  thumbs/             on-disk cover thumbnails
  app/                state, actions and views of the window
    theme/            design tokens (Radix Colors, spacing, radii) and fonts
    icons/            Lucide icons
    widgets/          the Figma library's components
```

## Plugins

Get books (in the sidebar) searches and browses book sources and adds
what you pick to your library. Project Gutenberg is built in; anyone can
add a source as a plugin: a `plugin.json` and a JavaScript file, run
sandboxed inside Dalibre. See
[Writing Dalibre plugins](docs/plugins/README.md), also in the CLI
(`npx dalibre-plugin@latest docs`), and the
[design](docs/plugins/design.md).

## MCP

Your own agents (Claude Code, Claude Desktop, Cursor…) can use the open
library over MCP: search it, edit metadata, add books, and convert
formats with their own tools. See
[Using Dalibre with your agents](docs/mcp/README.md) and the
[design](docs/mcp/design.md).

## Compatibility with calibre

- New libraries use calibre's own `metadata.db` schema (user_version 28).
- Dalibre registers the SQL functions calibre's triggers call (`title_sort`,
  `uuid4`, …), so writes behave as calibre's do.
- Book folders and files follow calibre's naming (`Author/Title (id)/Title -
  Author.ext`, transliterated to ASCII), and move when the title or first
  author changes.
- Edits write `metadata.opf` and mark the book in `metadata_dirtied`, so
  calibre refreshes it next time.
- Removed books go to the system Recycle Bin.

## Licenses

`internal/calibre/metadata_sqlite.sql` comes from calibre (GPL-3.0). Fonts
(Inter, Geist Mono) are under the SIL Open Font License; icons
are Lucide (ISC); colors are Radix Colors (MIT).
