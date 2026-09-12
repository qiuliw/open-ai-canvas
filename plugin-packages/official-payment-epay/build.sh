#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
package_file="$root_dir/../official-payment-epay.yingce-plugin"

node "$root_dir/../embed-documentation.mjs" official-payment-epay
binary="$root_dir/target/release/yingce-payment-epay"
cargo rustc --locked --release -- -C target-feature=+crt-static
install -Dm755 "$binary" "$root_dir/backend/provider"
rm -f "$package_file"
python3 - "$root_dir" "$package_file" <<'PY'
import os, sys, zipfile
root, output = sys.argv[1:]
with zipfile.ZipFile(output, "w", zipfile.ZIP_DEFLATED) as package:
    for name in ("manifest.json", "README.md", "docs/interface.md", "backend/provider"):
        package.write(os.path.join(root, name), name)
PY
