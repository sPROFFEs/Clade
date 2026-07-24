# Releasing PrAImate on GitHub

Manual release guide for the project. Run everything from the repo root
(`C:\Users\user\Downloads\PrAImate` or wherever your checkout lives).

The in-app installer and self-updater resolve assets from
`GET https://api.github.com/repos/sPROFFEs/praimate/releases/latest`, so the release must be a
**published release** (not draft, not pre-release) and asset file names must
match exactly what the code requests (see the asset list below).

## 0. Push the code

```powershell
git push origin main
```

## 1. Prepare the GUI frontend

```bash
cd cmd/praimate-gui/frontend
npm install
npm run build
cd ../../..
```

The platform build in step 4 compiles the native Linux GUI and both Windows
GUI architectures after this shared frontend is ready.

## 2. Stage PrAImate Code

The binaries use the vendored OpenCode v1.17.20 source plus the PrAImate
rebrand. Build each supported target explicitly:

```bash
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64 \
              windows-amd64 windows-arm64; do
  PRAIMATE_CODE_TARGET="$target" OUT="dist/$target" \
    bash scripts/build-praimate-code.sh
done

# Baseline/no-AVX2 builds are required for amd64 targets.
for target in linux-amd64 darwin-amd64 windows-amd64; do
  PRAIMATE_CODE_TARGET="$target" BASELINE=1 OUT="dist/$target" \
    bash scripts/build-praimate-code.sh
done
```

Keep each default binary inside its platform directory so the platform
archive bundles it as a sidecar. Then stage the standalone release assets
using the `praimate-code-<os>-<arch>` naming documented below.

```bash
for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
  cp "dist/$target/praimate-code" "dist/praimate-code-$target"
done
for target in windows-amd64 windows-arm64; do
  cp "dist/$target/praimate-code.exe" "dist/praimate-code-$target.exe"
done
cp dist/linux-amd64/praimate-code-baseline \
   dist/praimate-code-linux-amd64-baseline
cp dist/darwin-amd64/praimate-code-baseline \
   dist/praimate-code-darwin-amd64-baseline
cp dist/windows-amd64/praimate-code-baseline.exe \
   dist/praimate-code-windows-amd64-baseline.exe
```

## 3. Build graphify

Graphify is currently published as a native Linux amd64 standalone; other
platforms use the pinned `uv` installer fallback.

```bash
OUT=dist/linux-amd64 bash scripts/build-graphify.sh
cp dist/linux-amd64/praimate-graphify dist/praimate-graphify-linux-amd64
```

## 4. Build the platform archives

```bash
scripts/build.sh --version=1.0.10 --with-gui
```

This cross-compiles `praimate` + `wpc` for windows/linux/darwin and writes
`dist\praimate-<os>-<arch>.zip|.tar.gz`. The `-Version` value is stamped into
the binaries and **must equal the GitHub tag name** or the self-updater will
loop.

## 5. Checksums

```bash
cd dist
rm -f *.sha256 SHA256SUMS
for f in praimate-code-* praimate-graphify-* \
         praimate-darwin-amd64.tar.gz praimate-darwin-arm64.tar.gz \
         praimate-linux-amd64.tar.gz praimate-linux-arm64.tar.gz \
         praimate-windows-amd64.zip praimate-windows-arm64.zip; do
  sha256sum "$f" > "$f.sha256"
done
cat *.sha256 > SHA256SUMS
```

## 6. Create the release on GitHub

### Option A — web UI

Releases → **New Release** → tag `1.0.10` (target `main`), title
`PrAImate 1.0.10`, drag every file from step 5 (all platform bundles and
standalone managed-tool assets, their `.sha256` files, and `SHA256SUMS`)
into the attachments box, then publish.

### Option B — API

Authenticate the GitHub CLI, then:

```bash
# create the release and upload every asset
cd dist
gh release create 1.0.10 \
  praimate-*.zip praimate-*.tar.gz praimate-code-* praimate-graphify-* *.sha256 SHA256SUMS \
  --repo sPROFFEs/praimate \
  --target main \
  --title "PrAImate 1.0.10" \
  --generate-notes
```

## 7. Required asset names (what the code looks up)

| Asset | Consumer |
|---|---|
| `praimate-windows-{amd64,arm64}.zip` (+ per-OS tarballs) | self-updater, install scripts |
| `praimate-code-<os>-<arch>[.exe]` | CLIs-tab / `praimate code` install |
| `praimate-code-<os>-amd64-baseline[.exe]` | same, on hosts without AVX2 (VMs, old CPUs) |
| `praimate-graphify-linux-amd64` | managed graphify/RAG installation |
| `*.sha256`, `SHA256SUMS` | manual verification |

## 8. Verify

```powershell
.\dist\windows-amd64\praimate.exe -check-update   # should say you're current
```

and in the GUI: CLIs tab → PrAImate Code → Install… should download from
`github.com/sPROFFEs/praimate` and end with `✓ verified: <version>`.
