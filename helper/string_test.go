package helper

import (
	"reflect"
	"testing"
)

// TestSplitLines 测试字符串按行分割
func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "Unix 风格换行符 (LF)",
			input: "line1\nline2\nline3",
			want:  []string{"line1", "line2", "line3"},
		},
		{
			name:  "Windows 风格换行符 (CRLF)",
			input: "line1\r\nline2\r\nline3",
			want:  []string{"line1", "line2", "line3"},
		},
		{
			name:  "单行文本无换行符",
			input: "single line",
			want:  []string{"single line"},
		},
		{
			name:  "空字符串",
			input: "",
			want:  []string{""},
		},
		{
			name:  "只有换行符 (LF)",
			input: "\n",
			want:  []string{"", ""},
		},
		{
			name:  "只有换行符 (CRLF)",
			input: "\r\n",
			want:  []string{"", ""},
		},
		{
			name:  "多个连续换行符 (LF)",
			input: "line1\n\n\nline2",
			want:  []string{"line1", "", "", "line2"},
		},
		{
			name:  "多个连续换行符 (CRLF)",
			input: "line1\r\n\r\n\r\nline2",
			want:  []string{"line1", "", "", "line2"},
		},
		{
			name:  "末尾有换行符 (LF)",
			input: "line1\nline2\n",
			want:  []string{"line1", "line2", ""},
		},
		{
			name:  "末尾有换行符 (CRLF)",
			input: "line1\r\nline2\r\n",
			want:  []string{"line1", "line2", ""},
		},
		{
			name:  "开头有换行符 (LF)",
			input: "\nline1\nline2",
			want:  []string{"", "line1", "line2"},
		},
		{
			name:  "开头有换行符 (CRLF)",
			input: "\r\nline1\r\nline2",
			want:  []string{"", "line1", "line2"},
		},
		{
			name:  "包含空格和制表符",
			input: "  line1  \n\tline2\t\nline3",
			want:  []string{"  line1  ", "\tline2\t", "line3"},
		},
		{
			name:  "包含特殊字符",
			input: "Hello, 世界!\nこんにちは\n안녕하세요",
			want:  []string{"Hello, 世界!", "こんにちは", "안녕하세요"},
		},
		{
			name:  "混合 CRLF 优先",
			input: "line1\r\nline2\nline3",
			want:  []string{"line1", "line2\nline3"},
		},
		{
			name:  "长文本 (LF)",
			input: "This is line 1\nThis is line 2\nThis is line 3\nThis is line 4\nThis is line 5",
			want:  []string{"This is line 1", "This is line 2", "This is line 3", "This is line 4", "This is line 5"},
		},
		{
			name:  "长文本 (CRLF)",
			input: "This is line 1\r\nThis is line 2\r\nThis is line 3\r\nThis is line 4\r\nThis is line 5",
			want:  []string{"This is line 1", "This is line 2", "This is line 3", "This is line 4", "This is line 5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitLines(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitLines() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSplitLines_RealWorldExamples 测试真实场景示例
func TestSplitLines_RealWorldExamples(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "IP 列表 (Unix 格式)",
			input: "192.168.1.1\n192.168.1.2\n192.168.1.3",
			want:  []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"},
		},
		{
			name:  "IP 列表 (Windows 格式)",
			input: "192.168.1.1\r\n192.168.1.2\r\n192.168.1.3",
			want:  []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"},
		},
		{
			name:  "配置文件内容",
			input: "# Comment\r\nkey1=value1\r\nkey2=value2\r\n",
			want:  []string{"# Comment", "key1=value1", "key2=value2", ""},
		},
		{
			name:  "日志输出",
			input: "[INFO] Starting application\n[DEBUG] Loading config\n[INFO] Application started",
			want:  []string{"[INFO] Starting application", "[DEBUG] Loading config", "[INFO] Application started"},
		},
		{
			name:  "CSV 数据行",
			input: "name,age,city\r\nAlice,25,Beijing\r\nBob,30,Shanghai",
			want:  []string{"name,age,city", "Alice,25,Beijing", "Bob,30,Shanghai"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitLines(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitLines() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSplitLines_EdgeCases 测试边界情况
func TestSplitLines_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "只有空格",
			input: "   ",
			want:  []string{"   "},
		},
		{
			name:  "只有制表符",
			input: "\t\t\t",
			want:  []string{"\t\t\t"},
		},
		{
			name:  "CR 但不是 CRLF",
			input: "line1\rline2",
			want:  []string{"line1\rline2"},
		},
		{
			name:  "LF 紧跟 CR",
			input: "line1\n\rline2",
			want:  []string{"line1", "\rline2"},
		},
		{
			name:  "多个空行 (LF)",
			input: "\n\n\n",
			want:  []string{"", "", "", ""},
		},
		{
			name:  "多个空行 (CRLF)",
			input: "\r\n\r\n\r\n",
			want:  []string{"", "", "", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitLines(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitLines() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSplitLines_PrioritizeCRLF 测试 CRLF 优先逻辑
func TestSplitLines_PrioritizeCRLF(t *testing.T) {
	// 如果输入包含 \r\n，则优先使用 \r\n 分割
	// 这意味着单独的 \n 不会被当作分隔符
	input := "line1\r\nline2\nstill line2\r\nline3"
	want := []string{"line1", "line2\nstill line2", "line3"}

	got := SplitLines(input)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitLines() = %v, want %v", got, want)
	}
}

// TestSplitLines_Consistency 测试一致性
func TestSplitLines_Consistency(t *testing.T) {
	tests := []string{
		"line1\nline2\nline3",
		"line1\r\nline2\r\nline3",
		"",
		"single",
		"\n",
		"\r\n",
	}

	for _, input := range tests {
		result1 := SplitLines(input)
		result2 := SplitLines(input)

		if !reflect.DeepEqual(result1, result2) {
			t.Errorf("SplitLines() 返回不一致结果: %v != %v", result1, result2)
		}
	}
}

// TestSplitLines_LengthCheck 测试返回切片长度
func TestSplitLines_LengthCheck(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantLength int
	}{
		{
			name:       "3 行 (LF)",
			input:      "a\nb\nc",
			wantLength: 3,
		},
		{
			name:       "3 行 (CRLF)",
			input:      "a\r\nb\r\nc",
			wantLength: 3,
		},
		{
			name:       "1 行",
			input:      "single",
			wantLength: 1,
		},
		{
			name:       "空字符串",
			input:      "",
			wantLength: 1, // strings.Split("", "\n") 返回 [""]
		},
		{
			name:       "5 个空行 (LF)",
			input:      "\n\n\n\n",
			wantLength: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitLines(tt.input)
			if len(got) != tt.wantLength {
				t.Errorf("SplitLines() 长度 = %d, want %d", len(got), tt.wantLength)
			}
		})
	}
}

// BenchmarkSplitLines_LF Unix 风格换行符性能测试
func BenchmarkSplitLines_LF(b *testing.B) {
	input := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitLines(input)
	}
}

// BenchmarkSplitLines_CRLF Windows 风格换行符性能测试
func BenchmarkSplitLines_CRLF(b *testing.B) {
	input := "line1\r\nline2\r\nline3\r\nline4\r\nline5\r\nline6\r\nline7\r\nline8\r\nline9\r\nline10"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitLines(input)
	}
}

// BenchmarkSplitLines_LongText 长文本性能测试
func BenchmarkSplitLines_LongText(b *testing.B) {
	// 生成 1000 行文本
	var lines []string
	for i := 0; i < 1000; i++ {
		lines = append(lines, "This is a test line with some content")
	}
	input := ""
	for _, line := range lines {
		input += line + "\n"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SplitLines(input)
	}
}

// BenchmarkSplitLines_EmptyString 空字符串性能测试
func BenchmarkSplitLines_EmptyString(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SplitLines("")
	}
}
