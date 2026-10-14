#!/bin/sh
# Installs veraPDF, the PDF/A validator, into a directory (default
# ./verapdf-install). The version is pinned and the download is checked
# against its SHA-256; to move to a newer veraPDF, change both below. It needs
# Java 11 or newer on the PATH.
set -eu

version=1.30.3
sha256=b512bf24945503a33fc10738ecffc1f2dccaaf8593a448e94ee9ac86a9559a59
url=https://software.verapdf.org/releases/1.30/verapdf-greenfield-$version-installer.zip

target=$(mkdir -p "${1:-verapdf-install}" && cd "${1:-verapdf-install}" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl --fail --silent --show-error --location --output "$work/installer.zip" "$url"
echo "$sha256  $work/installer.zip" | shasum -a 256 -c - >/dev/null ||
	{ echo "veraPDF $version: the download does not match its checksum" >&2; exit 1; }
unzip -q "$work/installer.zip" -d "$work"

# The installer is unattended with a script of its answers
cat > "$work/auto-install.xml" <<XML
<AutomatedInstallation langpack="eng">
  <com.izforge.izpack.panels.htmlhello.HTMLHelloPanel id="welcome"/>
  <com.izforge.izpack.panels.target.TargetPanel id="install_dir">
    <installpath>$target</installpath>
  </com.izforge.izpack.panels.target.TargetPanel>
  <com.izforge.izpack.panels.packs.PacksPanel id="package_selection"/>
  <com.izforge.izpack.panels.install.InstallPanel id="install"/>
  <com.izforge.izpack.panels.finish.FinishPanel id="finish"/>
</AutomatedInstallation>
XML
java -jar "$work/verapdf-greenfield-$version/verapdf-izpack-installer-$version.jar" "$work/auto-install.xml" >/dev/null
echo "veraPDF $version installed: $target/verapdf"
