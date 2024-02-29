#!/usr/bin/env bash
set -eou pipefail

RELEASES_URL="https://github.com/lukeshay/deployer/releases"
FILE_BASENAME="deployer"
LATEST="$(curl -fsSL 'https://api.github.com/repos/lukeshay/deployer/tags' | jq -r '.[0].name')"

test -z "${INSTALL_DIR-}" && INSTALL_DIR="./deployer"
test -z "${VERSION-}" && VERSION="$LATEST"

OS="$(uname -s)"
ARCH="$(uname -m)"
TAR_FILE="$(echo -n "${FILE_BASENAME}_${OS}_${ARCH}.tar.gz" | tr '[:upper:]' '[:lower:]')"

(
	mkdir -p "$INSTALL_DIR"
	cd "$INSTALL_DIR"
	echo "Downloading Deployer $VERSION..."
	curl -sfLO "$RELEASES_URL/download/$VERSION/$TAR_FILE"
	curl -sfLO "$RELEASES_URL/download/$VERSION/checksums.txt"
	echo "Verifying checksums..."
	sha256sum --ignore-missing --quiet --check checksums.txt
	if command -v cosign >/dev/null 2>&1; then
		echo "Verifying signatures..."
		cosign verify-blob \
			--certificate-identity-regexp "https://github.com/lukeshay/deployer.*/.github/workflows/.*.yml@refs/tags/$VERSION" \
			--certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
			--cert "$RELEASES_URL/download/$VERSION/checksums.txt.pem" \
			--signature "$RELEASES_URL/download/$VERSION/checksums.txt.sig" \
			checksums.txt
	else
		echo "Could not verify signatures, cosign is not installed."
	fi
)

tar -xf "$INSTALL_DIR/$TAR_FILE" -C "$INSTALL_DIR"

echo "Ready to run $INSTALL_DIR/deployer"
