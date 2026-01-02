package update

import (
	"testing"
)

func TestNewVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    *Version
		wantErr bool
	}{
		{
			name:    "标准语义化版本",
			version: "1.2.3",
			want:    &Version{major: 1, minor: 2, patch: 3},
			wantErr: false,
		},
		{
			name:    "带v前缀的版本",
			version: "v1.2.3",
			want:    &Version{major: 1, minor: 2, patch: 3},
			wantErr: false,
		},
		{
			name:    "只有主版本号",
			version: "1",
			want:    &Version{major: 1, minor: 0, patch: 0},
			wantErr: false,
		},
		{
			name:    "主版本号和次版本号",
			version: "2.5",
			want:    &Version{major: 2, minor: 5, patch: 0},
			wantErr: false,
		},
		{
			name:    "大版本号",
			version: "v10.20.30",
			want:    &Version{major: 10, minor: 20, patch: 30},
			wantErr: false,
		},
		{
			name:    "无效版本号 - 非数字",
			version: "abc",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "无效版本号 - 空字符串",
			version: "",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "带预发布标识的版本",
			version: "v1.2.3-alpha",
			want:    &Version{major: 1, minor: 2, patch: 3},
			wantErr: false,
		},
		{
			name:    "带构建元数据的版本",
			version: "v1.2.3+build123",
			want:    &Version{major: 1, minor: 2, patch: 3},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewVersion(tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.major != tt.want.major || got.minor != tt.want.minor || got.patch != tt.want.patch {
					t.Errorf("NewVersion() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestVersion_String(t *testing.T) {
	tests := []struct {
		name    string
		version *Version
		want    string
	}{
		{
			name:    "标准版本",
			version: &Version{major: 1, minor: 2, patch: 3},
			want:    "1.2.3",
		},
		{
			name:    "零版本",
			version: &Version{major: 0, minor: 0, patch: 0},
			want:    "0.0.0",
		},
		{
			name:    "大版本号",
			version: &Version{major: 100, minor: 200, patch: 300},
			want:    "100.200.300",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.version.String(); got != tt.want {
				t.Errorf("Version.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersion_GreaterThan(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want bool
	}{
		{
			name: "主版本号大",
			v1:   "2.0.0",
			v2:   "1.9.9",
			want: true,
		},
		{
			name: "次版本号大",
			v1:   "1.5.0",
			v2:   "1.4.9",
			want: true,
		},
		{
			name: "修订号大",
			v1:   "1.0.5",
			v2:   "1.0.4",
			want: true,
		},
		{
			name: "版本相等",
			v1:   "1.2.3",
			v2:   "1.2.3",
			want: false,
		},
		{
			name: "版本较小",
			v1:   "1.2.3",
			v2:   "1.2.4",
			want: false,
		},
		{
			name: "带v前缀比较",
			v1:   "v2.0.0",
			v2:   "v1.0.0",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := NewVersion(tt.v1)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v1, err)
			}
			v2, err := NewVersion(tt.v2)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v2, err)
			}

			if got := v1.GreaterThan(v2); got != tt.want {
				t.Errorf("Version.GreaterThan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersion_GreaterThanOrEqual(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want bool
	}{
		{
			name: "版本大于",
			v1:   "2.0.0",
			v2:   "1.0.0",
			want: true,
		},
		{
			name: "版本相等",
			v1:   "1.2.3",
			v2:   "1.2.3",
			want: true,
		},
		{
			name: "版本小于",
			v1:   "1.0.0",
			v2:   "2.0.0",
			want: false,
		},
		{
			name: "次版本相等",
			v1:   "1.5.0",
			v2:   "1.5.0",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := NewVersion(tt.v1)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v1, err)
			}
			v2, err := NewVersion(tt.v2)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v2, err)
			}

			if got := v1.GreaterThanOrEqual(v2); got != tt.want {
				t.Errorf("Version.GreaterThanOrEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersion_Compare(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want int // -1: v1 < v2, 0: v1 == v2, 1: v1 > v2
	}{
		{
			name: "v1 大于 v2",
			v1:   "2.0.0",
			v2:   "1.0.0",
			want: 1,
		},
		{
			name: "v1 等于 v2",
			v1:   "1.2.3",
			v2:   "1.2.3",
			want: 0,
		},
		{
			name: "v1 小于 v2",
			v1:   "1.0.0",
			v2:   "2.0.0",
			want: -1,
		},
		{
			name: "次版本号不同",
			v1:   "1.5.0",
			v2:   "1.4.0",
			want: 1,
		},
		{
			name: "修订号不同",
			v1:   "1.0.1",
			v2:   "1.0.2",
			want: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := NewVersion(tt.v1)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v1, err)
			}
			v2, err := NewVersion(tt.v2)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v2, err)
			}

			if got := v1.compare(v2); got != tt.want {
				t.Errorf("Version.compare() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersion_Equal(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want bool
	}{
		{
			name: "版本相等",
			v1:   "1.2.3",
			v2:   "1.2.3",
			want: true,
		},
		{
			name: "版本不相等",
			v1:   "1.2.3",
			v2:   "1.2.4",
			want: false,
		},
		{
			name: "带 v 前缀的相等版本",
			v1:   "v1.0.0",
			v2:   "1.0.0",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := NewVersion(tt.v1)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v1, err)
			}
			v2, err := NewVersion(tt.v2)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v2, err)
			}

			if got := v1.Equal(v2); got != tt.want {
				t.Errorf("Version.Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersion_LessThan(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want bool
	}{
		{
			name: "主版本号小",
			v1:   "1.0.0",
			v2:   "2.0.0",
			want: true,
		},
		{
			name: "次版本号小",
			v1:   "1.3.0",
			v2:   "1.5.0",
			want: true,
		},
		{
			name: "修订号小",
			v1:   "1.0.1",
			v2:   "1.0.5",
			want: true,
		},
		{
			name: "版本相等",
			v1:   "1.2.3",
			v2:   "1.2.3",
			want: false,
		},
		{
			name: "版本较大",
			v1:   "2.0.0",
			v2:   "1.0.0",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := NewVersion(tt.v1)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v1, err)
			}
			v2, err := NewVersion(tt.v2)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v2, err)
			}

			if got := v1.LessThan(v2); got != tt.want {
				t.Errorf("Version.LessThan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersion_LessThanOrEqual(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want bool
	}{
		{
			name: "版本小于",
			v1:   "1.0.0",
			v2:   "2.0.0",
			want: true,
		},
		{
			name: "版本相等",
			v1:   "1.2.3",
			v2:   "1.2.3",
			want: true,
		},
		{
			name: "版本大于",
			v1:   "2.0.0",
			v2:   "1.0.0",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1, err := NewVersion(tt.v1)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v1, err)
			}
			v2, err := NewVersion(tt.v2)
			if err != nil {
				t.Fatalf("NewVersion(%s) error = %v", tt.v2, err)
			}

			if got := v1.LessThanOrEqual(v2); got != tt.want {
				t.Errorf("Version.LessThanOrEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVersion_Accessors(t *testing.T) {
	v, err := NewVersion("1.2.3")
	if err != nil {
		t.Fatalf("NewVersion() error = %v", err)
	}

	if got := v.Major(); got != 1 {
		t.Errorf("Version.Major() = %v, want 1", got)
	}

	if got := v.Minor(); got != 2 {
		t.Errorf("Version.Minor() = %v, want 2", got)
	}

	if got := v.Patch(); got != 3 {
		t.Errorf("Version.Patch() = %v, want 3", got)
	}
}
