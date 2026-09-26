package linux_packages_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func requirePackageRepoTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("required tool %q not in PATH; skipping", tool)
		}
	}
}

// newPackageTestKey creates an isolated GNUPGHOME holding a single
// passphrase-less RSA signing key and returns an environment for commands
// that must use it.
func newPackageTestKey(t *testing.T) []string {
	t.Helper()
	gnupgHome := filepath.Join(t.TempDir(), "gnupg")
	if err := os.Mkdir(gnupgHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Stop the agent holding sockets inside the temp directory.
		_ = exec.Command("gpgconf", "--homedir", gnupgHome, "--kill", "gpg-agent").Run()
	})
	env := append(os.Environ(), "GNUPGHOME="+gnupgHome)
	params := `Key-Type: RSA
Key-Length: 2048
Name-Real: Clarified Labs Test Packages
Name-Email: packages-test@example.invalid
Expire-Date: 0
%no-protection
%commit
`
	cmd := exec.Command("gpg", "--batch", "--gen-key")
	cmd.Env = env
	cmd.Stdin = strings.NewReader(params)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate test signing key: %v\n%s", err, output)
	}
	return env
}

// writeTestDistributions seeds a scratch reprepro base directory with the same
// conf/distributions content as this repository, including the org-wide Label.
func writeTestDistributions(t *testing.T, repoDir string) {
	t.Helper()
	confDir := filepath.Join(repoDir, "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const distributions = `Origin: Clarified Labs, Inc.
Label: clarifiedlabs
Suite: stable
Codename: stable
Components: main
Architectures: amd64 arm64
SignWith: yes
`
	if err := os.WriteFile(filepath.Join(confDir, "distributions"), []byte(distributions), 0o644); err != nil {
		t.Fatal(err)
	}
}

func buildDummyDeb(t *testing.T, distDir, name, version, arch string) {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "usr", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/bin/sh\necho "+name+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	debDir := filepath.Join(root, "DEBIAN")
	if err := os.Mkdir(debDir, 0o755); err != nil {
		t.Fatal(err)
	}
	control := "Package: " + name + "\n" +
		"Version: " + version + "\n" +
		"Section: utils\n" +
		"Priority: optional\n" +
		"Architecture: " + arch + "\n" +
		"Maintainer: Clarified Labs Test Packages <packages-test@example.invalid>\n" +
		"Description: test package " + name + "\n"
	if err := os.WriteFile(filepath.Join(debDir, "control"), []byte(control), 0o644); err != nil {
		t.Fatal(err)
	}
	debPath := filepath.Join(distDir, name+"_"+version+"_"+arch+".deb")
	cmd := exec.Command("dpkg-deb", "--build", "--root-owner-group", root, debPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb --build %s: %v\n%s", name, err, output)
	}
}

// buildDummySignedRpm builds a single GPG-signed test rpm with rpmbuild. The
// product repositories own payload signing, so linux-packages only needs a
// representative signed rpm to exercise rpm-repo-update.sh.
func buildDummySignedRpm(t *testing.T, env []string, distDir, name, version, rpmArch string) string {
	t.Helper()
	topdir := filepath.Join(t.TempDir(), "rpmbuild")
	for _, dir := range []string{"BUILD", "BUILDROOT", "RPMS", "SOURCES", "SPECS"} {
		if err := os.MkdirAll(filepath.Join(topdir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	spec := filepath.Join(topdir, "SPECS", name+".spec")
	specContent := `Name: ` + name + `
Version: ` + version + `
Release: 1
Summary: test package
License: MIT
BuildArch: ` + rpmArch + `
AutoReqProv: no

%description
test package

%prep

%build

%install
mkdir -p %{buildroot}/usr/bin
printf '#!/bin/sh\necho ` + name + `\n' > %{buildroot}/usr/bin/` + name + `
chmod 0755 %{buildroot}/usr/bin/` + name + `

%files
/usr/bin/` + name + `
`
	if err := os.WriteFile(spec, []byte(specContent), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("rpmbuild", "--define", "_topdir "+topdir, "--target", rpmArch, "-bb", spec)
	build.Env = env
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("rpmbuild %s: %v\n%s", name, err, output)
	}
	rpms, err := filepath.Glob(filepath.Join(topdir, "RPMS", "*", "*.rpm"))
	if err != nil || len(rpms) != 1 {
		t.Fatalf("expected exactly one built rpm for %s, got %v (%v)", name, rpms, err)
	}
	keyName := ""
	listKeys := exec.Command("gpg", "--batch", "--with-colons", "--list-secret-keys")
	listKeys.Env = env
	output, err := listKeys.Output()
	if err == nil {
		for line := range strings.SplitSeq(string(output), "\n") {
			if fields := strings.Split(line, ":"); fields[0] == "uid" && len(fields) > 9 {
				keyName = fields[9]
			}
		}
	}
	if keyName == "" {
		t.Fatal("no GPG secret key available to sign the test rpm")
	}
	sign := exec.Command("rpmsign", "--addsign", "--define", "_gpg_name "+keyName, rpms[0])
	sign.Env = env
	if output, err := sign.CombinedOutput(); err != nil {
		t.Fatalf("rpm --addsign %s: %v\n%s", name, err, output)
	}
	dest := filepath.Join(distDir, name+"-"+version+"-1."+rpmArch+".rpm")
	if err := os.Rename(rpms[0], dest); err != nil {
		t.Fatal(err)
	}
	return dest
}

func countFilesWithSuffix(t *testing.T, root, suffix string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, suffix) {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestAptRepoUpdateScript(t *testing.T) {
	requirePackageRepoTools(t, "reprepro", "gpg", "dpkg-deb")
	env := newPackageTestKey(t)

	workDir := t.TempDir()
	repoRoot := filepath.Join(workDir, "linux-packages")
	repoDir := filepath.Join(repoRoot, "deb")
	writeTestDistributions(t, repoDir)

	distDir := filepath.Join(workDir, "dist")
	if err := os.Mkdir(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildDummyDeb(t, distDir, "test-one", "0.0.0", "amd64")
	buildDummyDeb(t, distDir, "test-two", "0.0.0", "arm64")

	runScript := func() {
		cmd := exec.Command("bash", "scripts/apt-repo-update.sh")
		cmd.Env = append(env, "REPO_DIR="+repoDir, "DIST_DIR="+distDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("apt-repo-update.sh: %v\n%s", err, output)
		}
	}
	runScript()

	// The signed InRelease metadata verifies against the test key and carries
	// the org-wide Label.
	verify := exec.Command("gpg", "--batch", "--verify", filepath.Join(repoDir, "dists", "stable", "InRelease"))
	verify.Env = env
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("gpg --verify InRelease: %v\n%s", err, output)
	}
	release, err := os.ReadFile(filepath.Join(repoDir, "dists", "stable", "InRelease"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(release), "Label: clarifiedlabs") {
		t.Fatalf("InRelease missing org-wide Label:\n%s", release)
	}

	check := exec.Command("reprepro", "-b", repoDir, "check")
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("reprepro check: %v\n%s", err, output)
	}

	list := func() string {
		t.Helper()
		cmd := exec.Command("reprepro", "-b", repoDir, "list", "stable")
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("reprepro list stable: %v\n%s", err, output)
		}
		return string(output)
	}
	listOut := list()
	for _, want := range []string{
		"stable|main|amd64: test-one 0.0.0",
		"stable|main|arm64: test-two 0.0.0",
	} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("reprepro list stable missing %q:\n%s", want, listOut)
		}
	}

	if got := countFilesWithSuffix(t, filepath.Join(repoDir, "pool"), ".deb"); got != 2 {
		t.Fatalf("expected 2 debs in pool, got %d", got)
	}

	// The repo root gained the exported org-wide keyring and the Pages marker.
	if info, err := os.Stat(filepath.Join(repoRoot, "clarifiedlabs-archive-keyring.asc")); err != nil || info.Size() == 0 {
		t.Fatalf("clarifiedlabs-archive-keyring.asc missing or empty: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, ".nojekyll")); err != nil {
		t.Fatalf(".nojekyll missing: %v", err)
	}

	// Re-running is a no-op: no error, no duplicates.
	runScript()
	if got := countFilesWithSuffix(t, filepath.Join(repoDir, "pool"), ".deb"); got != 2 {
		t.Fatalf("expected 2 debs in pool after re-run, got %d", got)
	}
	listOut = list()
	for _, pkg := range []string{"test-one", "test-two"} {
		if got := strings.Count(listOut, pkg); got != 1 {
			t.Fatalf("expected exactly one %s entry after re-run, got %d:\n%s", pkg, got, listOut)
		}
	}
}

func TestRpmRepoUpdateScript(t *testing.T) {
	requirePackageRepoTools(t, "rpmbuild", "rpm", "createrepo_c", "gpg")
	env := newPackageTestKey(t)

	workDir := t.TempDir()
	distDir := filepath.Join(workDir, "dist")
	if err := os.Mkdir(distDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rpmPath := buildDummySignedRpm(t, env, distDir, "test-one", "0.0.0", "x86_64")

	repoRoot := filepath.Join(workDir, "linux-packages")
	runUpdate := func() {
		cmd := exec.Command("bash", "scripts/rpm-repo-update.sh")
		cmd.Env = append(env, "REPO_DIR="+repoRoot, "DIST_DIR="+distDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rpm-repo-update.sh: %v\n%s", err, output)
		}
	}
	runUpdate()

	// The repo copy is byte-identical to the signed release artifact.
	repoRPM := filepath.Join(repoRoot, "rpm", "x86_64", "test-one-0.0.0-1.x86_64.rpm")
	if _, err := os.Stat(repoRPM); err != nil {
		t.Fatalf("expected %s: %v", repoRPM, err)
	}
	if got, want := sha256File(t, rpmPath), sha256File(t, repoRPM); got != want {
		t.Fatalf("repo rpm %s differs from the signed artifact (sha256 %s vs %s)", repoRPM, got, want)
	}

	repomdPath := filepath.Join(repoRoot, "rpm", "x86_64", "repodata", "repomd.xml")
	for _, path := range []string{repomdPath, repomdPath + ".asc"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
	verify := exec.Command("gpg", "--batch", "--verify", repomdPath+".asc", repomdPath)
	verify.Env = env
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("gpg --verify repomd.xml: %v\n%s", err, output)
	}

	// Re-running the repo update is a no-op: repomd.xml is untouched.
	repomdHash := sha256File(t, repomdPath)
	runUpdate()
	if after := sha256File(t, repomdPath); after != repomdHash {
		t.Fatal("repomd.xml changed on no-op re-run")
	}
}

// The repository's own published names must be org-wide and must not regress
// to the old harness-only names.
func TestRepoUsesOrgWideNames(t *testing.T) {
	repo, err := os.ReadFile("clarifiedlabs.repo")
	if err != nil {
		t.Fatal(err)
	}
	text := string(repo)
	for _, want := range []string{
		"[clarifiedlabs]",
		"name=Clarified Labs, Inc. packages",
		"gpgkey=https://clarifiedlabs.github.io/linux-packages/clarifiedlabs-archive-keyring.asc",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("clarifiedlabs.repo missing %q:\n%s", want, text)
		}
	}

	distributions, err := os.ReadFile("deb/conf/distributions")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(distributions), "Label: clarifiedlabs") {
		t.Errorf("deb/conf/distributions missing org-wide Label:\n%s", distributions)
	}

	keyring := "clarifiedlabs-archive-keyring.asc"
	if _, err := os.Stat(keyring); err != nil {
		t.Errorf("%s missing: %v", keyring, err)
	}
	if _, err := os.Stat("harness-archive-keyring.asc"); err == nil {
		t.Error("harness-archive-keyring.asc should not exist")
	}

	scripts, err := filepath.Glob(filepath.Join("scripts", "*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range scripts {
		info, err := os.Stat(script)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable (mode %v)", script, info.Mode().Perm())
		}
		if body, err := os.ReadFile(script); err != nil {
			t.Fatal(err)
		} else if strings.Contains(string(body), "harness-archive-keyring.asc") {
			t.Errorf("%s still exports the old keyring name", script)
		}
	}
}
