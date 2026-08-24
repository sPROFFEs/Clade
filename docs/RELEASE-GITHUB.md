# Releasing PrAImate on GitHub

PrAImate 1.1 and later is a GUI-only desktop application for Linux and
Windows. macOS and GUI-less archives are not release targets.

The installer and updater resolve the latest published release from
`sPROFFEs/praimate`, so version strings, the Git tag, and asset names must
agree exactly.

## 1. Version

Update all three defaults:

- `internal/version/version.go`
- `scripts/build.sh`
- `scripts/build.ps1`

## 2. Platform bundles

On Linux amd64:

```bash
rm -rf dist
PATH="$HOME/.bun/bin:$HOME/.local/bin:$PATH" \
  bash scripts/build.sh --version=1.2.2 --with-code --with-graphify
```

This produces the native Linux amd64 GUI bundle and cross-compiled Windows
amd64/arm64 GUI bundles. A Linux arm64 GUI bundle must be built on a native
Linux arm64 host; the build refuses to publish a GUI-less archive.

On Windows, `scripts/build.ps1` builds the Windows amd64 and arm64 GUI bundles.

## 3. Managed PrAImate Code assets

Build the supported standalone binaries explicitly. Baseline builds are
required for amd64 systems without AVX2:

```bash
for target in linux-amd64 linux-arm64 windows-amd64 windows-arm64; do
  PRAIMATE_CODE_TARGET="$target" OUT="dist/$target" \
    bash scripts/build-praimate-code.sh
done

for target in linux-amd64 windows-amd64; do
  PRAIMATE_CODE_TARGET="$target" BASELINE=1 OUT="dist/$target" \
    bash scripts/build-praimate-code.sh
done

cp dist/linux-amd64/praimate-code dist/praimate-code-linux-amd64
cp dist/linux-amd64/praimate-code-baseline dist/praimate-code-linux-amd64-baseline
cp dist/linux-arm64/praimate-code dist/praimate-code-linux-arm64
cp dist/windows-amd64/praimate-code.exe dist/praimate-code-windows-amd64.exe
cp dist/windows-amd64/praimate-code-baseline.exe dist/praimate-code-windows-amd64-baseline.exe
cp dist/windows-arm64/praimate-code.exe dist/praimate-code-windows-arm64.exe
```

Graphify is published as a native Linux amd64 binary. Other platforms use
the pinned `uv` installation fallback:

```bash
OUT=dist/linux-amd64 bash scripts/build-graphify.sh
cp dist/linux-amd64/praimate-graphify dist/praimate-graphify-linux-amd64
```

## 4. Checksums

Create one sidecar per uploaded asset plus the aggregate manifest:

```bash
cd dist
rm -f SHA256SUMS *.sha256
for file in \
  praimate-linux-amd64.tar.gz \
  praimate-windows-amd64.zip \
  praimate-windows-arm64.zip \
  praimate-code-linux-amd64 \
  praimate-code-linux-amd64-baseline \
  praimate-code-linux-arm64 \
  praimate-code-windows-amd64.exe \
  praimate-code-windows-amd64-baseline.exe \
  praimate-code-windows-arm64.exe \
  praimate-graphify-linux-amd64
do
  sha256sum "$file" | tee "$file.sha256" >> SHA256SUMS
done
```

## 5. Publish

Create a published release, not a draft or prerelease:

```bash
git tag -a 1.2.2 -m "PrAImate 1.2.2"
git push origin main
git push origin 1.2.2

gh release create 1.2.2 \
  --repo sPROFFEs/praimate \
  --target main \
  --title "PrAImate 1.2.2" \
  --notes-file RELEASE_NOTES.md

gh release upload 1.2.2 --clobber \
  dist/praimate-linux-amd64.tar.gz \
  dist/praimate-windows-amd64.zip \
  dist/praimate-windows-arm64.zip \
  dist/praimate-code-linux-amd64 \
  dist/praimate-code-linux-amd64-baseline \
  dist/praimate-code-linux-arm64 \
  dist/praimate-code-windows-amd64.exe \
  dist/praimate-code-windows-amd64-baseline.exe \
  dist/praimate-code-windows-arm64.exe \
  dist/praimate-graphify-linux-amd64 \
  dist/*.sha256 \
  dist/SHA256SUMS
```

## Required asset names

| Asset | Consumer |
|---|---|
| `praimate-linux-amd64.tar.gz` | Linux installer and updater |
| `praimate-windows-{amd64,arm64}.zip` | Windows installer and updater |
| `praimate-code-<os>-<arch>[.exe]` | Managed PrAImate Code installer |
| `praimate-code-<os>-amd64-baseline[.exe]` | Managed installer on non-AVX2 amd64 hosts |
| `praimate-graphify-linux-amd64` | Managed Graphify/RAG installer |
| `*.sha256`, `SHA256SUMS` | Release verification |

After publishing, verify `praimate -check-update` reports the installed
`1.2.2` build as current and that the GitHub release is neither draft nor
prerelease.
