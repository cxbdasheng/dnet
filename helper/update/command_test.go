package update

import (
	"strings"
	"testing"
)

func TestCheckAndUpdateRejectsInvalidCurrentVersionWithoutNetwork(t *testing.T) {
	err := CheckAndUpdate("invalid-version", false)
	if err == nil {
		t.Fatal("CheckAndUpdate() error = nil, want invalid-version error")
	}
	if !strings.Contains(err.Error(), "版本格式不正确") {
		t.Fatalf("CheckAndUpdate() error = %q, want 版本格式不正确", err)
	}
}
