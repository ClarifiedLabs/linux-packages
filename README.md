# Clarified Labs Linux packages

Org-wide signed APT and RPM repositories for Clarified Labs products, served by
GitHub Pages and updated by each product's release workflow:

- [`harness`](https://github.com/ClarifiedLabs/harness) (`harness`,
  `harness-model-proxy`, `harness-mcp-proxy`)
- [`mdcli`](https://github.com/ClarifiedLabs/mdcli) (`md`)

Both products share one signing key and one GitHub App; the repository-update
scripts live here so every product publishes through the same code path.

## Debian / Ubuntu (apt)

```sh
curl -fsSL https://clarifiedlabs.github.io/linux-packages/clarifiedlabs-archive-keyring.asc | sudo gpg --dearmor -o /usr/share/keyrings/clarifiedlabs-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/clarifiedlabs-archive-keyring.gpg] https://clarifiedlabs.github.io/linux-packages/deb stable main" | sudo tee /etc/apt/sources.list.d/clarifiedlabs.list
sudo apt-get update
sudo apt-get install md
sudo apt-get install harness harness-model-proxy harness-mcp-proxy
```

## Fedora / RHEL (dnf)

```sh
sudo curl -fsSL -o /etc/yum.repos.d/clarifiedlabs.repo https://clarifiedlabs.github.io/linux-packages/clarifiedlabs.repo
sudo dnf install md
sudo dnf install harness harness-model-proxy harness-mcp-proxy
```

All repository metadata and RPM packages are signed with the OpenPGP key published at
[`clarifiedlabs-archive-keyring.asc`](clarifiedlabs-archive-keyring.asc) and
referenced by `gpgkey=` in [`clarifiedlabs.repo`](clarifiedlabs.repo).

Products can be installed together from the same repository entry; package and
file names never collide across products.

## Updating the repositories

Product release workflows call these scripts from their checkout of this
repository:

- `scripts/apt-repo-update.sh` — adds the `.deb` packages in `DIST_DIR` to the
  `reprepro`-managed APT repository at `REPO_DIR` (`deb/`), re-exports the
  public keyring, and verifies the result.
- `scripts/rpm-repo-update.sh` — copies the `.rpm` packages in `DIST_DIR` into
  `rpm/$arch/`, regenerates `repodata/` with `createrepo_c`, and GPG-signs
  `repomd.xml`.

Both are idempotent: re-running a failed publish is safe, and a rebuilt tag
with the same version but different content is refused loudly instead of
silently replacing signed artifacts. RPM payload signing stays in the product
repositories so the GitHub release assets, `checksums.txt`, attestations, and
this repository serve byte-identical signed files.

## Migrating from the old harness-specific names

The repository previously used harness-specific names for the keyring, repo
file, and apt sources entry. One-time migration:

```sh
# dnf/yum: fetch the repo definition under its new name and drop the old one
sudo curl -fsSL -o /etc/yum.repos.d/clarifiedlabs.repo https://clarifiedlabs.github.io/linux-packages/clarifiedlabs.repo
sudo rm -f /etc/yum.repos.d/harness.repo
```

```sh
# apt: re-fetch the keyring to the new path and update signed-by= in sources.list
curl -fsSL https://clarifiedlabs.github.io/linux-packages/clarifiedlabs-archive-keyring.asc | sudo gpg --dearmor -o /usr/share/keyrings/clarifiedlabs-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/clarifiedlabs-archive-keyring.gpg] https://clarifiedlabs.github.io/linux-packages/deb stable main" | sudo tee /etc/apt/sources.list.d/clarifiedlabs.list
sudo rm -f /usr/share/keyrings/harness-archive-keyring.gpg /etc/apt/sources.list.d/harness.list
sudo apt-get update
```

The signing key is unchanged; only the file and label names changed. The apt
repository URL and the rpm `baseurl` are unchanged.
