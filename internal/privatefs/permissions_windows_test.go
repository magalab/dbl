//go:build windows

package privatefs

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestRestrictSetsCurrentUserOnlyProtectedACL(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(file, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path      string
		directory bool
	}{{dir, true}, {file, false}} {
		if err := Restrict(test.path, test.directory); err != nil {
			t.Fatalf("Restrict(%q): %v", test.path, err)
		}
		assertPrivateACL(t, test.path, test.directory)
	}
}

func assertPrivateACL(t *testing.T, path string, directory bool) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("DACL is not protected from inherited broad permissions")
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 1 {
		t.Fatalf("DACL ACE count = %v, want exactly one current-user grant", dacl)
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil {
		t.Fatal(err)
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Mask != windows.ACCESS_MASK(windows.GENERIC_ALL) {
		t.Fatalf("ACE type/mask = %d/%#x, want allowed GENERIC_ALL", ace.Header.AceType, ace.Mask)
	}
	user, err := currentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if equal := windows.EqualSid(aceSID, user); !equal {
		t.Fatal("ACL trustee is not the current user")
	}
	wantInheritance := uint8(0)
	if directory {
		wantInheritance = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	}
	if ace.Header.AceFlags != wantInheritance {
		t.Fatalf("ACE inheritance flags = %#x, want %#x", ace.Header.AceFlags, wantInheritance)
	}
}

func currentUserSID() (*windows.SID, error) {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}
