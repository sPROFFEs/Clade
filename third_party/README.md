# Vendored third-party sources

PrAImate ships two bundled tools that are built from third-party source.
That source is **vendored here** so the builds are self-contained — they
no longer clone from upstream at build time, and we control the exact
tree we compile. Both are MIT licensed; their original `LICENSE` files
are kept verbatim (the build also emits `PRAIMATE-*-NOTICE` sidecars
recording provenance).

| Dir | Upstream | Pinned | License | Built into |
|---|---|---|---|---|
| `opencode/` | https://github.com/sst/opencode | `v1.17.3` | MIT (© opencode) | **PrAImate Code** (`scripts/build-praimate-code.sh`) |
| `graphify/` | https://github.com/safishamsi/graphify (`graphifyy` on PyPI) | `0.8.36` | MIT (© Safi Shamsi) | **praimate-graphify** (`scripts/build-graphify.sh`) |

## opencode/

A pristine mirror of OpenCode at the pinned tag (`.git` and
`node_modules` stripped). The PrAImate **rebrand is applied at build
time**, not baked into the vendored tree — `build-praimate-code.sh`
copies this source to a scratch dir, runs `praimate-code-rebrand.sh`
over it, `bun install`s, and compiles. Keeping the mirror pristine means
the rebrand diff stays reviewable in one small script and bumping
upstream is a clean re-vendor.

**Re-vendor / bump upstream:**

```sh
ref=v1.17.4   # new tag
git clone --depth 1 --branch "$ref" https://github.com/sst/opencode /tmp/oc
rm -rf /tmp/oc/.git
rm -rf third_party/opencode && mv /tmp/oc third_party/opencode
# then bump OPENCODE_REF's default note in build-praimate-code.sh
```

`bun install` still pulls the JS dependency tree from npm at build time
(node_modules is intentionally not vendored — it's gigabytes and is
reproducible from the committed `bun.lock`).

## graphify/

The exact `graphifyy==0.8.36` sdist source (PyPI), with the build
artifact `*.egg-info` removed. `build-graphify.sh` installs this local
package (`uv pip install ./third_party/graphify[openai]`) and freezes it
with PyInstaller. graphify's own Python dependencies (openai,
tree-sitter, …) still resolve from PyPI at build — vendoring the full
transitive dependency tree is out of scope.

**Re-vendor / bump version:** download the new sdist from PyPI, extract,
drop `*.egg-info`, replace `third_party/graphify/`, and bump
`GRAPHIFY_PIN`'s default note in `build-graphify.sh`.
