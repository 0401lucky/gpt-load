package health

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// resetHintMargin 补偿上游文案只精确到分钟带来的边界误差。
	resetHintMargin = time.Minute
	// minResetHintCooldown 与 maxResetHintCooldown 是可解析时长的固定内置上下限。
	minResetHintCooldown = time.Minute
	maxResetHintCooldown = 24 * time.Hour
)

var (
	// resetHintAnchor 是唯一被认可的锚点，不做「任意 in + 时长」的泛化匹配。
	resetHintAnchor = regexp.MustCompile(`(?i)try again in\s*`)
	// resetHintSegment 匹配紧跟锚点或前一片段的「数字 + 单位」。
	resetHintSegment = regexp.MustCompile(`^[\s,]*([0-9]+)\s*([dwhmsDWHMS])`)
)

// ParseResetHint 从上游限流文案里解析「稍后重试」时长。
//
// 只有锚点 `try again in` 之后的「数字 + 单位」片段会被消费，单位支持 d/w/h/m/s；
// 至少命中一个片段才算解析成功。命中时返回的时长已叠加固定余量并钳制到
// [minResetHintCooldown, maxResetHintCooldown]。未命中时返回 false，调用方必须
// 回退到默认冷却，不产生任何新副作用。
func ParseResetHint(summary string) (time.Duration, bool) {
	anchor := resetHintAnchor.FindStringIndex(summary)
	if anchor == nil {
		return 0, false
	}
	remainder := summary[anchor[1]:]
	total := time.Duration(0)
	matched := false
	for {
		segment := resetHintSegment.FindStringSubmatch(remainder)
		if segment == nil {
			break
		}
		total += resetHintSegmentDuration(segment[1], segment[2])
		matched = true
		remainder = remainder[len(segment[0]):]
		// 抵达上限后继续累加不会改变钳制结果。
		if total >= maxResetHintCooldown {
			break
		}
	}
	if !matched || total <= 0 {
		return 0, false
	}
	return clampResetHintCooldown(total + resetHintMargin), true
}

func resetHintSegmentDuration(amount string, unit string) time.Duration {
	unitDuration := resetHintUnitDuration(unit)
	count, err := strconv.ParseInt(amount, 10, 64)
	// 单个片段溢出，或本身已超过上限时直接顶到上限，交由统一的钳制逻辑收敛。
	// 不能把数值截断到某个较小的固定值：那会让冷却短于上游给出的重置时间。
	if err != nil || count > int64(maxResetHintCooldown/unitDuration) {
		return maxResetHintCooldown
	}
	return time.Duration(count) * unitDuration
}

func resetHintUnitDuration(unit string) time.Duration {
	switch strings.ToLower(unit) {
	case "d":
		return 24 * time.Hour
	case "w":
		return 7 * 24 * time.Hour
	case "h":
		return time.Hour
	case "m":
		return time.Minute
	default:
		return time.Second
	}
}

func clampResetHintCooldown(value time.Duration) time.Duration {
	if value > maxResetHintCooldown {
		return maxResetHintCooldown
	}
	if value < minResetHintCooldown {
		return minResetHintCooldown
	}
	return value
}
