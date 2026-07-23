package vmname

import "testing"

func TestPrefixed(t *testing.T) {
	tests := []struct {
		owner string
		name  string
		want  string
	}{
		{owner: "fw190", name: "mysql-test", want: "fw190-mysql-test"},
		{owner: "fw190", name: "fw190-mysql-test", want: "fw190-mysql-test"},
		{owner: " fw190 ", name: " mysql-test ", want: "fw190-mysql-test"},
	}
	for _, test := range tests {
		if got := Prefixed(test.owner, test.name); got != test.want {
			t.Fatalf("Prefixed(%q, %q) = %q, want %q", test.owner, test.name, got, test.want)
		}
	}
}

func TestValidatePVE(t *testing.T) {
	valid := []string{
		"a",
		"mysql-test",
		"MySQL-test",
		"ubuntu-server-24.04-lts",
		"fw190-mysql-test",
	}
	for _, name := range valid {
		if err := ValidatePVE(name); err != nil {
			t.Errorf("ValidatePVE(%q) returned %v", name, err)
		}
	}

	invalid := []string{
		"",
		"mysql_test",
		"-mysql",
		"mysql-",
		"mysql..test",
		"mysql test",
		"数据库",
	}
	for _, name := range invalid {
		if err := ValidatePVE(name); err == nil {
			t.Errorf("ValidatePVE(%q) unexpectedly succeeded", name)
		}
	}
}

func TestValidateManagedChecksFinalName(t *testing.T) {
	if err := ValidateManaged("valid-owner", "mysql-test"); err != nil {
		t.Fatalf("valid managed name returned %v", err)
	}
	if err := ValidateManaged("invalid_owner", "mysql-test"); err == nil {
		t.Fatal("owner containing an underscore unexpectedly succeeded")
	}
	if err := ValidateManaged("valid-owner", "mysql_test"); err == nil {
		t.Fatal("VM name containing an underscore unexpectedly succeeded")
	}
}
