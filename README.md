# Clarified Labs Linux packages

Signed APT and RPM repositories for
[harness](https://github.com/ClarifiedLabs/harness) releases, served by GitHub
Pages and updated by the harness release workflow.

## Debian / Ubuntu (apt)

```sh
curl -fsSL https://clarifiedlabs.github.io/linux-packages/harness-archive-keyring.asc | sudo gpg --dearmor -o /usr/share/keyrings/harness-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/harness-archive-keyring.gpg] https://clarifiedlabs.github.io/linux-packages/deb stable main" | sudo tee /etc/apt/sources.list.d/harness.list
sudo apt-get update
sudo apt-get install harness harness-model-proxy harness-mcp-proxy
```

## Fedora / RHEL (dnf)

```sh
sudo curl -fsSL -o /etc/yum.repos.d/harness.repo https://clarifiedlabs.github.io/linux-packages/harness.repo
sudo dnf install harness harness-model-proxy harness-mcp-proxy
```

All repository metadata and RPM packages are signed with the
`Clarified Labs, Inc. Packages <hello@clarified.io>` OpenPGP key, published at
[`harness-archive-keyring.asc`](harness-archive-keyring.asc).
