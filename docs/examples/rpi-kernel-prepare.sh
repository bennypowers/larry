#!/bin/sh
#
# rpi-kernel — custom prepare script for Raspberry Pi kernel ebuilds.
#
# This is a reference implementation for the `larry prepare` escape hatch.
# Place it at <category>/<package>/files/prepare.sh and make it executable.
#
# Usage:
#   larry prepare sys-kernel/raspberrypi-kernel/raspberrypi-kernel-6.18.46_p20260824.ebuild
#
# The script extracts the firmware date from PV (e.g. 6.18.46_p20260824),
# fetches the kernel git_hash from the raspberrypi/firmware GitHub release,
# and rewrites the COMMIT= line in the ebuild.
#
# NOTE: Manifest generation and rollback are handled by the larry framework
# (internal/prepare). This script only updates COMMIT=.
#

set -e

# Extract firmware date from PV (e.g. 6.18.32_p20260521 -> 20260521).
fw_date=$(echo "$PV" | grep -oP '_p\K\d{8}')
if [ -z "$fw_date" ]; then
    echo "ERROR: Cannot extract firmware date from version: $PV" >&2
    exit 1
fi

fw_tag="1.$fw_date"

# Look up kernel commit from firmware release.
commit=$(curl -sfL \
    "https://raw.githubusercontent.com/raspberrypi/firmware/$fw_tag/extra/git_hash" \
    2>/dev/null | tr -d '[:space:]')
if [ -z "$commit" ]; then
    echo "ERROR: Cannot fetch kernel commit for firmware $fw_tag" >&2
    exit 1
fi

# Validate commit hash.
if ! echo "$commit" | grep -qP '^[0-9a-f]{40}$'; then
    echo "ERROR: Invalid commit hash from firmware $fw_tag: $commit" >&2
    exit 1
fi

echo "Firmware $fw_tag -> commit $commit"

# Update COMMIT= line in ebuild.
if ! grep -q '^COMMIT=' "$EBUILD"; then
    echo "ERROR: No COMMIT= line found in ebuild" >&2
    exit 1
fi

sed -i "s|^COMMIT=.*|COMMIT=\"$commit\"|" "$EBUILD"
echo "Updated COMMIT in ebuild"
