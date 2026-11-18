package app

import (
	"testing"

	"github.com/magalab/dbl/internal/credentials"
	"github.com/magalab/dbl/internal/preferences"
)

func TestStalePasswordReadDoesNotRevealPasswordInReopenedEditor(t *testing.T) {
	app := newTestWorkbench()
	profile := preferences.ConnectionProfile{ID: "saved", CredentialRef: "v1:0123456789abcdef0123456789abcdef"}
	app.loadConnectionProfile(profile)
	app.showConnectionEditor = true
	app.profileSecretLoading = true
	staleRequest := app.profileSecretRequestID

	app.showConnectionEditor = false
	app.discardConnectionEditorSecrets()
	app.loadConnectionProfile(profile)
	app.showConnectionEditor = true
	app.profileSecretLoading = true

	app.finishProfileSecretRead(staleRequest, []byte("saved-password"), nil)
	if app.profileSecretVisible || app.profileSecret != "" || !app.profileSecretLoading {
		t.Fatal("the old password read changed the reopened editor")
	}

	app.finishProfileSecretRead(app.profileSecretRequestID, []byte("saved-password"), nil)
	if !app.profileSecretVisible || !app.profileSecretFromVault || app.profileSecret == "" || app.profileSecretLoading {
		t.Fatal("the current password read did not reveal the saved password")
	}
	app.toggleProfileSecretVisibility()
	if app.profileSecretVisible || app.profileSecret != "" {
		t.Fatal("hiding the saved password left it in the form")
	}
}

func TestProfileSecretPlaceholderShowsSavedPasswordMask(t *testing.T) {
	app := newTestWorkbench()
	if got := app.profileSecretPlaceholder(); got != "输入密码" {
		t.Fatalf("placeholder without a saved password = %q", got)
	}
	app.profileCredential = "v1:0123456789abcdef0123456789abcdef"
	if got := app.profileSecretPlaceholder(); got != "••••••••" {
		t.Fatalf("placeholder with a saved password = %q", got)
	}
}

func TestRevealedVaultPasswordDoesNotChangeProfileTestFingerprint(t *testing.T) {
	app := newTestWorkbench()
	app.profileCredential = "v1:0123456789abcdef0123456789abcdef"
	app.profileSecret = "saved-secret"
	app.profileSecretFromVault = true
	revealed := app.connectionProfileTestFingerprint()
	app.profileSecret = ""
	app.profileSecretFromVault = false
	if blank := app.connectionProfileTestFingerprint(); revealed != blank {
		t.Fatalf("revealing a saved password changed the test fingerprint: %q != %q", revealed, blank)
	}
}

func TestPreparingConnectionTestKeepsRevealedVaultCredentialReference(t *testing.T) {
	backend := &cleanupTestBackend{values: make(map[string][]byte), failed: make(map[string]bool)}
	vault := credentials.New(backend)
	ref, err := vault.Store([]byte("saved-secret"))
	if err != nil {
		t.Fatal(err)
	}
	app := newTestWorkbench()
	app.vault = vault
	app.profileCredential = string(ref)
	app.profileSecret = "saved-secret"
	app.profileSecretFromVault = true

	profile, cleanup, err := app.prepareConnectionProfileTest()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if profile.CredentialRef != string(ref) {
		t.Fatalf("credential reference = %q, want original %q", profile.CredentialRef, ref)
	}
	if len(backend.values) != 1 {
		t.Fatalf("vault entries after preparing test = %d, want only saved credential", len(backend.values))
	}
}
