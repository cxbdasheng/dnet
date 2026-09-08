package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 在 init() 中创建的正则表达式的编译版本被缓存在这里，这样
// 它只需要被创建一次。
var versionRegex *regexp.Regexp

// semVerRegex 是用于解析语义化版本的正则表达式。
const semVerRegex string = `v?([0-9]+)(\.[0-9]+)?(\.[0-9]+)?` +
	`(-([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?` +
	`(\+([0-9A-Za-z\-]+(\.[0-9A-Za-z\-]+)*))?`

// Version 表示单独的语义化版本。
type Version struct {
	major, minor, patch uint64
	prerelease          []string
	metadata            string
}

func init() {
	versionRegex = regexp.MustCompile("^" + semVerRegex + "$")
}

// NewVersion 解析给定的版本并返回 Version 实例，如果
// 无法解析该版本则返回错误。如果版本是类似于 SemVer 的版本，则会
// 尝试将其转换为 SemVer。
func NewVersion(v string) (*Version, error) {
	m := versionRegex.FindStringSubmatch(v)
	if m == nil {
		return nil, fmt.Errorf("%s 不是语义化版本", v)
	}

	sv := &Version{}

	var err error
	sv.major, err = parseVersionSegment(m[1], "主版本号")
	if err != nil {
		return nil, err
	}

	sv.minor, err = parseVersionSegment(strings.TrimPrefix(m[2], "."), "次版本号")
	if err != nil {
		return nil, err
	}

	sv.patch, err = parseVersionSegment(strings.TrimPrefix(m[3], "."), "修订号")
	if err != nil {
		return nil, err
	}

	if m[5] != "" {
		sv.prerelease = strings.Split(m[5], ".")
		for _, identifier := range sv.prerelease {
			if isNumericIdentifier(identifier) && len(identifier) > 1 && identifier[0] == '0' {
				return nil, fmt.Errorf("预发布数字标识符 %q 不能包含前导零", identifier)
			}
		}
	}
	sv.metadata = m[8]

	return sv, nil
}

// parseVersionSegment 解析版本号的单个部分（主版本号、次版本号或修订号）
func parseVersionSegment(s, segmentName string) (uint64, error) {
	if s == "" {
		return 0, nil
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("%s %q 不能包含前导零", segmentName, s)
	}
	val, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析%s时出错：%s", segmentName, err)
	}
	return val, nil
}

// Major 返回主版本号
func (v Version) Major() uint64 {
	return v.major
}

// Minor 返回次版本号
func (v Version) Minor() uint64 {
	return v.minor
}

// Patch 返回修订号
func (v Version) Patch() uint64 {
	return v.patch
}

// String 将 Version 对象转换为字符串。
// 注意，如果原始版本包含前缀 v，则转换后的版本将不包含 v。
// 根据规范，语义版本不包含前缀 v，而在实现上则是可选的。
func (v Version) String() string {
	result := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if len(v.prerelease) > 0 {
		result += "-" + strings.Join(v.prerelease, ".")
	}
	if v.metadata != "" {
		result += "+" + v.metadata
	}
	return result
}

// Equal 测试两个版本是否相等
func (v Version) Equal(o *Version) bool {
	return v.compare(o) == 0
}

// GreaterThan 测试一个版本是否大于另一个版本。
func (v Version) GreaterThan(o *Version) bool {
	return v.compare(o) > 0
}

// GreaterThanOrEqual 测试一个版本是否大于或等于另一个版本。
func (v Version) GreaterThanOrEqual(o *Version) bool {
	return v.compare(o) >= 0
}

// LessThan 测试一个版本是否小于另一个版本
func (v Version) LessThan(o *Version) bool {
	return v.compare(o) < 0
}

// LessThanOrEqual 测试一个版本是否小于或等于另一个版本
func (v Version) LessThanOrEqual(o *Version) bool {
	return v.compare(o) <= 0
}

// compare 按 SemVer 规则比较两个版本。构建元数据不影响优先级。
func (v Version) compare(o *Version) int {
	if d := compareSegment(v.major, o.major); d != 0 {
		return d
	}
	if d := compareSegment(v.minor, o.minor); d != 0 {
		return d
	}
	if d := compareSegment(v.patch, o.patch); d != 0 {
		return d
	}
	return comparePrerelease(v.prerelease, o.prerelease)
}

func compareSegment(v, o uint64) int {
	if v < o {
		return -1
	}
	if v > o {
		return 1
	}
	return 0
}

func comparePrerelease(v, o []string) int {
	if len(v) == 0 && len(o) == 0 {
		return 0
	}
	if len(v) == 0 {
		return 1
	}
	if len(o) == 0 {
		return -1
	}

	for i := 0; i < len(v) && i < len(o); i++ {
		if d := comparePrereleaseIdentifier(v[i], o[i]); d != 0 {
			return d
		}
	}
	if len(v) < len(o) {
		return -1
	}
	if len(v) > len(o) {
		return 1
	}
	return 0
}

func comparePrereleaseIdentifier(v, o string) int {
	vNumeric, oNumeric := isNumericIdentifier(v), isNumericIdentifier(o)
	if vNumeric && oNumeric {
		if len(v) < len(o) {
			return -1
		}
		if len(v) > len(o) {
			return 1
		}
	} else if vNumeric {
		return -1
	} else if oNumeric {
		return 1
	}

	if v < o {
		return -1
	}
	if v > o {
		return 1
	}
	return 0
}

func isNumericIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}
	for _, r := range identifier {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
