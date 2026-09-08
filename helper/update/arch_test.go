package update

import "testing"

func TestBuildTargetArchiveNameContract(t *testing.T) {
	tests := []struct {
		target BuildTarget
		want   string
	}{
		{BuildTarget{GOOS: "android", GOARCH: "arm64"}, "dnet_2.1.0_android_arm64.tar.gz"},
		{BuildTarget{GOOS: "darwin", GOARCH: "amd64"}, "dnet_2.1.0_darwin_x86_64.tar.gz"},
		{BuildTarget{GOOS: "darwin", GOARCH: "arm64"}, "dnet_2.1.0_darwin_arm64.tar.gz"},
		{BuildTarget{GOOS: "freebsd", GOARCH: "386"}, "dnet_2.1.0_freebsd_i386.tar.gz"},
		{BuildTarget{GOOS: "freebsd", GOARCH: "amd64"}, "dnet_2.1.0_freebsd_x86_64.tar.gz"},
		{BuildTarget{GOOS: "freebsd", GOARCH: "arm", GOARM: "5"}, "dnet_2.1.0_freebsd_armv5.tar.gz"},
		{BuildTarget{GOOS: "freebsd", GOARCH: "arm", GOARM: "6"}, "dnet_2.1.0_freebsd_armv6.tar.gz"},
		{BuildTarget{GOOS: "freebsd", GOARCH: "arm", GOARM: "7"}, "dnet_2.1.0_freebsd_armv7.tar.gz"},
		{BuildTarget{GOOS: "freebsd", GOARCH: "arm64"}, "dnet_2.1.0_freebsd_arm64.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "386"}, "dnet_2.1.0_linux_i386.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "amd64"}, "dnet_2.1.0_linux_x86_64.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "arm", GOARM: "5"}, "dnet_2.1.0_linux_armv5.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "arm", GOARM: "6"}, "dnet_2.1.0_linux_armv6.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "arm", GOARM: "7"}, "dnet_2.1.0_linux_armv7.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "arm64"}, "dnet_2.1.0_linux_arm64.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mips", GOMIPS: "hardfloat"}, "dnet_2.1.0_linux_mips_hardfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mips", GOMIPS: "softfloat"}, "dnet_2.1.0_linux_mips_softfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mipsle", GOMIPS: "hardfloat"}, "dnet_2.1.0_linux_mipsle_hardfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mipsle", GOMIPS: "softfloat"}, "dnet_2.1.0_linux_mipsle_softfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mips64", GOMIPS64: "hardfloat"}, "dnet_2.1.0_linux_mips64_hardfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mips64", GOMIPS64: "softfloat"}, "dnet_2.1.0_linux_mips64_softfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mips64le", GOMIPS64: "hardfloat"}, "dnet_2.1.0_linux_mips64le_hardfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "mips64le", GOMIPS64: "softfloat"}, "dnet_2.1.0_linux_mips64le_softfloat.tar.gz"},
		{BuildTarget{GOOS: "linux", GOARCH: "riscv64"}, "dnet_2.1.0_linux_riscv64.tar.gz"},
		{BuildTarget{GOOS: "windows", GOARCH: "386"}, "dnet_2.1.0_windows_i386.zip"},
		{BuildTarget{GOOS: "windows", GOARCH: "amd64"}, "dnet_2.1.0_windows_x86_64.zip"},
		{BuildTarget{GOOS: "windows", GOARCH: "arm64"}, "dnet_2.1.0_windows_arm64.zip"},
	}

	if len(tests) != 27 {
		t.Fatalf("synthetic contract has %d cases, want 27", len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := tt.target.archiveName("2.1.0")
			if err != nil {
				t.Fatalf("archiveName() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("archiveName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildTargetARMWithFloatSetting(t *testing.T) {
	got, err := (BuildTarget{GOOS: "linux", GOARCH: "arm", GOARM: "7,hardfloat"}).archiveName("1.0.0")
	if err != nil {
		t.Fatalf("archiveName() error = %v", err)
	}
	if got != "dnet_1.0.0_linux_armv7.tar.gz" {
		t.Fatalf("archiveName() = %q", got)
	}
}

func TestBuildTargetMIPSABIFailsClosed(t *testing.T) {
	for _, target := range []BuildTarget{
		{GOOS: "linux", GOARCH: "mips"},
		{GOOS: "linux", GOARCH: "mipsle", GOMIPS: "unknown"},
		{GOOS: "linux", GOARCH: "mips64"},
		{GOOS: "linux", GOARCH: "mips64le", GOMIPS64: "unknown"},
	} {
		if _, err := target.archiveName("1.0.0"); err == nil {
			t.Fatalf("archiveName(%+v) error = nil, want fail-closed error", target)
		}
	}
}

func TestBuildTargetRiscv64HasSingleName(t *testing.T) {
	got, err := (BuildTarget{GOOS: "linux", GOARCH: "riscv64"}).archiveName("1.0.0")
	if err != nil {
		t.Fatalf("archiveName() error = %v", err)
	}
	if got != "dnet_1.0.0_linux_riscv64.tar.gz" {
		t.Fatalf("archiveName() = %q", got)
	}
}
