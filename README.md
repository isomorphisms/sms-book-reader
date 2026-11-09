# SMS Book Reader — mock jog

A small Go reader for flip phones. **There is no SMS gateway yet.** Commands run locally or through HTTP so `curl` or ICU can exercise the same reader without choosing an SMS provider.

One bookmark **per reader and book**. A short undo history is attached to each bookmark; these are not additional named cursors. Reader records are plain JSON files, replaced atomically, in `SMS_READER_HOME` (default `~/.local/state/sms-book-reader`). The server serializes commands in one process. This mock is **not** a multi-process transactional service.

## Run locally

Requires Go 1.23 or later.

```sh
go test ./...
go build -o sms-book-reader .
export SMS_READER_HOME="$(mktemp -d)"
./sms-book-reader seed                  # ada, ben, cy
./sms-book-reader books
./sms-book-reader jog ada 'select 1'
./sms-book-reader jog ada continue
./sms-book-reader jog ada back
./sms-book-reader jog ada 'half back'
./sms-book-reader jog ada undo
./sms-book-reader state ada
```

## Curl / ICU (both use HTTP POST with a plain-text command)

Start a local server, then use another shell:

```sh
./sms-book-reader serve                    # 127.0.0.1:8787 by default
curl -s http://127.0.0.1:8787/books
curl -s --data-binary 'continue' http://127.0.0.1:8787/jog/ada
curl -s http://127.0.0.1:8787/state/ada
icu get http://127.0.0.1:8787/books
icu post http://127.0.0.1:8787/jog/ada continue
```

The server is **unauthenticated and bound to loopback by default**. Do not expose it publicly. User names here are fake IDs, not phone numbers.

## Mock SMS commands

| Input | Effect |
|---|---|
| `BOOKS`, `LIST` | Numbered book catalog |
| `SELECT 2`, `READ nostromo` | Change active book, retaining its own bookmark |
| `CONTINUE`, `C`, `N`, `NEXT`, `GO`, `YES` | Advance one structural passage |
| `BACK`, `B` | Previous structural passage |
| `HALF BACK`, `HB` | Second half of preceding passage (word boundary) |
| `RESTART` | First passage of current book |
| `UNDO`, `U` | Restore previous position, up to 20 steps |
| `AGAIN`, `A` | Repeat current passage without moving |
| `HELP`, `?` | Command list |

Every mock paragraph, stanza, scene, or verse group has a stable ID and is delivered as a unit. Embedded verse line breaks are preserved. There is **no SMS-size splitting yet**; choosing message lengths can wait for a real flip-phone test.

## Books and licensing

`catalog/books.json` contains the discussed reading list, **not the complete works**. Each entry has short **original mock text** and explicit structural passage breaks. We cannot invent the user's Dante translation, and modern copyrighted translations and works (e.g., Borges and J. L. Carr) cannot be republished without rights. Full texts should only replace fixtures when the correct edition and redistribution permissions are established. Pirsig is intentionally absent.

## Cat Food / Android

The build workflow compiles Linux host and Android ARMv7 (A1) / ARM64 (C67) candidates using Go and Android NDK linkers, runs Go tests, and uploads versioned CI artifacts with checksums. Cat Food's workbench inventory should track this repository and its Android delivery ledger should report a **gap** until exact binaries have been published with verified digest/receipt. CI compilation is not a Cat Food installation or physical C67 acceptance. Devices should install tested artifacts, not compile locally.