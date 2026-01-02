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

	return sv, nil
}

// parseVersionSegment 解析版本号的单个部分（主版本号、次版本号或修订号）
func parseVersionSegment(s, segmentName string) (uint64, error) {
	if s == "" {
		return 0, nil
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
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
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

// compare 比较当前版本与另一个版本。如果当前版本小于另一个版本则返回 -1；如果两个版本相等则返回 0；如果当前版本大于另一个版本，则返回 1。
//
// 版本比较是基于 X.Y.Z 格式进行的。
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

	return 0
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
