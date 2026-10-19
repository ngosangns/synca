# How this app is built

Synca follows the same construction as [Zed](https://github.com/zed-industries/zed): one headless core, a thin composition root, and a window shell that does not own the rules. Zed's editor is Rust and GPUI because a web view could not hold its frame budget. Synca copies skills and MCP config. That work is file IO. The core stays the Go module. The window stays SwiftUI. GPUI is GPL-3.0 and is not a dependency.

## Dependency direction

```
synca-app           crates/zed           menus, actions, which dock is open
   │  WindowShell   crates/workspace     banner, left dock, center pane, right dock, status, toast, modal
   │  Design        crates/ui + theme    tokens and components
   ▼
SyncaKit           crates/client        argv, JSON, plan text, the log of commands it ran
   │
   ▼
synca (Go)         crates/project       scan, diff, install, sync. The source of truth.
```

`synca-app` may import `SyncaKit` and `NSShell`. `SyncaKit` imports neither SwiftUI nor the app. The Go module does not import either Swift target. Feature views call `AppModel`. `AppModel` calls `SyncaKit`, which runs the `synca` binary. The app does not reimplement a sync.

`NSShell` is the private package [ns-shell](https://github.com/ngosangns/ns-shell), checked out beside this repo. It holds the tokens, components, toast, confirmation, and window layers shared with hearth and promptscope. This repo keeps sync commands and the Go core.

Zed's `cli` crate is a second binary that finds the running app and speaks IPC. Synca inverts that, on purpose: the CLI is the product, and the window is a client of the CLI. Both still share one implementation of the rules.

## What the Go module is

| Package | Zed analogue | Owns |
| --- | --- | --- |
| `cmd/synca` | `crates/cli` `main` | Process entry. Exits non-zero and prints the error. |
| `internal/cli` | `crates/cli` | Flag parsing and stdout. No copy of the sync rules. |
| `internal/core` | `crates/project` | Discover agents, read skills and MCP, plan a sync, write it. |

`internal/core` stays one package until a second caller needs a seam. Splitting it into a crate per agent would copy Zed's `languages/*` layout without Zed's number of languages.

## Window

| Zed | This window |
| --- | --- |
| Left dock | `SidebarView` — scope, projects, skills, MCP |
| Center `Pane` | `DetailView` — the open item, or a flow (install, add, plan, update) |
| Right dock | `ActivityView` or `SettingsView`, one at a time, plus the button strip that can open either |
| `StatusBar` | in-flight operation and CLI version |
| Banner | `CLIMissingBanner` when the binary is missing |
| `toast_layer` | `Toast` |
| `modal_layer` | `confirmationDialog`, and the project sheet |

`SyncaApp` owns `AppModel` (`@MainActor`). CLI calls run inside `Task`s so the window stays on the foreground executor. `HearthCommand`-style values live in `SyncaCommand`: the menu binds them and does not contain the action.

There is no daemon. Synca does not supervise processes. Do not add one to match Hearth or Zed's collab server.

## Scope

In scope: skills and MCP for the agents listed in the README, user scope and project scope, a dry-run plan before a write, and a macOS window over the CLI.

Out of scope: GPUI, a command palette, an extension host, collaboration, editing file contents in the window (the detail view shows a preview and a diff), and a second sync engine in Swift.

## GitHub

`ci.yml` builds the Go module first and only then runs the Swift tests against that binary, which is the same gate Zed uses when the app must not be tested against a stale CLI. `release.yml` publishes the one darwin-arm64 asset. Composite actions are not worth a third file until a step is copied into a second workflow.
