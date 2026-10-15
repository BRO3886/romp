#!/bin/sh
set -eu

romp_tag=v2.0.0-experimental.3
romp_prefix="${HOME}/.local"
romp_base=https://github.com/BRO3886/romp/releases/download
while [ "$#" -gt 0 ]; do
  case "$1" in
    --prefix|--version|--release-base-url)
      [ "$#" -ge 2 ] || { printf '%s\n' "Missing value for $1" >&2; exit 1; }
      case "$1" in
        --prefix) romp_prefix=$2 ;;
        --version) romp_tag=$2 ;;
        --release-base-url) romp_base=$2 ;;
      esac
      shift 2 ;;
    --help)
      printf '%s\n' 'Install experimental Romp as ~/.local/bin/romp' 'Usage: sh install.sh [--prefix DIR] [--version v2.0.0-experimental.N]'
      exit 0 ;;
    *) printf '%s\n' "Unknown option: $1" >&2; exit 1 ;;
  esac
done

case "$romp_tag" in
  v2.0.0-experimental.*) romp_number=${romp_tag#v2.0.0-experimental.} ;;
  *) printf '%s\n' 'Expected a v2 experimental release tag' >&2; exit 1 ;;
esac
case "$romp_number" in ''|*[!0-9]*) printf '%s\n' 'Invalid release tag' >&2; exit 1 ;; esac
for romp_command in node npm curl tar; do
  command -v "$romp_command" >/dev/null 2>&1 || { printf '%s\n' "Install $romp_command first" >&2; exit 1; }
done
node -e 'const [major, minor] = process.versions.node.split(".").map(Number); if (major < 22 || (major === 22 && minor < 19)) { console.error("Node.js 22.19 or newer is required"); process.exit(1); }'
romp_prefix=$(node -e 'process.stdout.write(require("node:path").resolve(process.argv[1]))' "$romp_prefix")
romp_data="$romp_prefix/share/romp"
romp_bin="$romp_prefix/bin/romp"
if [ -e "$romp_bin" ] || [ -L "$romp_bin" ]; then
  if [ -L "$romp_bin" ] || [ ! -f "$romp_bin" ] || ! grep -qx '# romp-managed-v2-launcher' "$romp_bin"; then
    printf '%s\n' "Existing $romp_bin is not managed by this installer. Move it first or choose another --prefix" >&2
    exit 1
  fi
fi
if [ -e "$romp_data" ] || [ -L "$romp_data" ]; then
  if [ -L "$romp_data" ] || [ ! -f "$romp_data/.romp-install" ] || [ "$(cat "$romp_data/.romp-install")" != 'romp-v2' ]; then
    printf '%s\n' "Existing $romp_data is not managed by this installer" >&2
    exit 1
  fi
fi
mkdir -p "$romp_prefix/bin" "$romp_data/releases"
printf '%s\n' 'romp-v2' > "$romp_data/.romp-install"
romp_stage=$(mktemp -d "$romp_data/releases/$romp_tag.XXXXXX")
romp_temp=$(mktemp -d "${TMPDIR:-/tmp}/romp-install.XXXXXX")
romp_activated=false
romp_cleanup() {
  rm -rf "$romp_temp"
  if [ "$romp_activated" = false ]; then rm -rf "$romp_stage"; fi
}
trap romp_cleanup EXIT
trap 'exit 1' HUP INT TERM
romp_archive="romp-v2-${romp_tag#v}.tar.gz"
curl -fsSL --retry 3 --connect-timeout 15 --max-time 180 "$romp_base/$romp_tag/$romp_archive" -o "$romp_temp/$romp_archive"
curl -fsSL --retry 3 --connect-timeout 15 --max-time 180 "$romp_base/$romp_tag/SHA256SUMS" -o "$romp_temp/SHA256SUMS"
node - "$romp_temp" "$romp_archive" <<'NODE'
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const [directory, archive] = process.argv.slice(2);
const matches = fs.readFileSync(path.join(directory, 'SHA256SUMS'), 'utf8').trim().split('\n').map(line => line.trim().split(/\s+/)).filter(parts => parts[1] === archive);
const digest = crypto.createHash('sha256').update(fs.readFileSync(path.join(directory, archive))).digest('hex');
if (matches.length !== 1 || !/^[a-f0-9]{64}$/.test(matches[0][0]) || matches[0][0] !== digest) {
  throw new Error('Release checksum verification failed');
}
NODE
tar -xzf "$romp_temp/$romp_archive" --strip-components=1 -C "$romp_stage"
node - "$romp_stage" "${romp_tag#v}" <<'NODE'
const fs = require('node:fs');
const path = require('node:path');
const [directory, version] = process.argv.slice(2);
for (const file of ['package.json', 'release.json']) {
  if (JSON.parse(fs.readFileSync(path.join(directory, file), 'utf8')).version !== version) throw new Error('Release version mismatch');
}
NODE
(cd "$romp_stage" && npm ci --ignore-scripts)
node "$romp_stage/dist/cli.js" --help >/dev/null
node "$romp_stage/dist/cli.js" cloud --help >/dev/null
node - "$romp_prefix" "$romp_stage" <<'NODE'
const fs = require('node:fs');
const path = require('node:path');
const [prefix, stage] = process.argv.slice(2);
const data = path.join(prefix, 'share/romp');
const entry = path.join(data, 'current/dist/cli.js').replaceAll("'", "'\\''");
const launcher = path.join(prefix, 'bin', `.romp-${process.pid}`);
fs.writeFileSync(launcher, `#!/bin/sh\n# romp-managed-v2-launcher\nexec node '${entry}' "$@"\n`, { mode: 0o755, flag: 'wx' });
const link = path.join(data, `.current-${process.pid}`);
const current = path.join(data, 'current');
const previous = fs.existsSync(current) ? fs.readlinkSync(current) : undefined;
let switched = false;
try {
  fs.symlinkSync(stage, link);
  fs.renameSync(link, current);
  switched = true;
  fs.renameSync(launcher, path.join(prefix, 'bin/romp'));
} catch (error) {
  if (switched) {
    if (previous === undefined) fs.unlinkSync(current);
    else { fs.symlinkSync(previous, link); fs.renameSync(link, current); }
  }
  throw error;
} finally {
  fs.rmSync(link, { force: true });
  fs.rmSync(launcher, { force: true });
}
NODE
romp_activated=true
printf '%s\n' "Installed $romp_tag as $romp_bin" "Add $romp_prefix/bin to PATH, then run: romp --help"
