---
name: check-sing-box-deps
description: Use ONLY when checking or updating the upstream sing-box dependency for the subsing project. Triggers on phrases like "check sing-box updates", "update sing-box dependency", "upgrade sing-box", "is there a new sing-box version".
---

# Check and Update sing-box Dependencies

## Project Overview

**subsing** is a sing-box config converter. It has a **single direct dependency**: `github.com/sagernet/sing-box` in `go.mod`. It imports only two packages from sing-box:

- `github.com/sagernet/sing-box/option` — for outbound/endpoint config types
- `github.com/sagernet/sing-box/constant` — for protocol type constants

## Dependency Update Procedure

### Step 1: Find the current version

Read `go.mod` and note the `github.com/sagernet/sing-box` version line.

### Step 2: Check upstream for newer tags

```bash
git ls-remote --tags https://github.com/Sagernet/sing-box.git | sed 's/.*refs\/tags\///' | sort -V | tail -10
```

This lists tags sorted by version. Determine which versions are newer than the current one.

### Step 3: Check for breaking changes

The only packages subsing imports are `option` and `constant`. New versions are safe to update if:

- **Additive changes only**: New fields on existing structs, new type constants, new files — these are always safe.
- **No field type changes**: If a struct field changes type (e.g., `string` → `Listable[string]`), verify subsing doesn't directly access that field.
- **No renamed/removed exports**: If constants or type names are removed/renamed, this is breaking.

In practice, sing-box's alpha releases are very stable and subsing only uses a tiny surface of option.Outbound and option.Endpoint types. Breaking changes to those core types are extremely rare.

### Step 4: Update and verify

```bash
cd /workspaces/subsing
go get github.com/sagernet/sing-box@<new-version>
go mod tidy
go build ./...
go vet ./...
```

All four commands must succeed. If any fails, investigate the error — it likely means a breaking change in the `option` or `constant` packages.

### Step 5: Report

Report the version jump, any new features from release notes that might affect subsing, and whether the build passed.

## Key Types subsing Uses (from sing-box option package)

- `option.Outbound` — `.Tag`, `.Type`, and `Options` fields
- `option.Endpoint` — `.Tag`, `.Type`, and `Options` fields
- `option.WireGuardEndpointOptions`, `option.WireGuardPeer`
- `constant.Type*` — protocol type string constants

Changes to any other types in the `option` or `constant` packages do not affect subsing.
