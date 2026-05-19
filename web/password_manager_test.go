package web

import "testing"

func TestPasswordRecoveryCodeResetsPasswordOnce(t *testing.T) {
	pm, err := NewPasswordManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	defer pm.Close()

	if err := pm.SetPassword("admin", "old-password"); err != nil {
		t.Fatalf("SetPassword() error = %v", err)
	}

	code, err := pm.GenerateRecoveryCode("admin")
	if err != nil {
		t.Fatalf("GenerateRecoveryCode() error = %v", err)
	}
	if code == "" {
		t.Fatal("GenerateRecoveryCode() returned empty code")
	}

	if err := pm.RecoverPasswordWithCode("admin", code, "new-password"); err != nil {
		t.Fatalf("RecoverPasswordWithCode() error = %v", err)
	}

	valid, err := pm.VerifyPassword("admin", "new-password")
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !valid {
		t.Fatal("new password should be valid")
	}

	if err := pm.RecoverPasswordWithCode("admin", code, "another-password"); err == nil {
		t.Fatal("reusing recovery code should fail")
	}
}

func TestPasswordRecoveryCodeRotationInvalidatesPreviousCode(t *testing.T) {
	pm, err := NewPasswordManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewPasswordManager() error = %v", err)
	}
	defer pm.Close()

	if err := pm.SetPassword("admin", "old-password"); err != nil {
		t.Fatalf("SetPassword() error = %v", err)
	}

	oldCode, err := pm.GenerateRecoveryCode("admin")
	if err != nil {
		t.Fatalf("GenerateRecoveryCode() old error = %v", err)
	}
	newCode, err := pm.GenerateRecoveryCode("admin")
	if err != nil {
		t.Fatalf("GenerateRecoveryCode() new error = %v", err)
	}

	if err := pm.RecoverPasswordWithCode("admin", oldCode, "bad-password"); err == nil {
		t.Fatal("old recovery code should be inactive")
	}
	if err := pm.RecoverPasswordWithCode("admin", newCode, "new-password"); err != nil {
		t.Fatalf("new recovery code should work: %v", err)
	}
}
