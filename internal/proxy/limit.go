package proxy

import (
	"strings"
	"unicode/utf8"
)

// EstimateTokens is a local stand-in for a tokenizer. CJK-heavy text is
// closer to one token per rune, English closer to one per four bytes, so
// this uses about two runes per token.
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return (n + 1) / 2
}

type limiter struct {
	stop      []string
	maxTokens int
	hold      int
	held      string
	out       strings.Builder
	finish    string
	matched   string
	done      bool
}

func newLimiter(stop []string, maxTokens int) *limiter {
	var clean []string
	for _, s := range stop {
		if s != "" {
			clean = append(clean, s)
		}
	}
	hold := 0
	for _, s := range clean {
		n := utf8.RuneCountInString(s)
		if n > hold {
			hold = n
		}
	}
	if hold > 0 {
		hold--
	}
	return &limiter{stop: clean, maxTokens: maxTokens, hold: hold}
}

// Push accepts newly generated text and returns the portion safe to send.
func (l *limiter) Push(s string) (emit string, done bool) {
	if l == nil || l.done || s == "" {
		if l == nil {
			return s, false
		}
		return "", l.done
	}
	l.held += s
	if l.maxTokens > 0 {
		total := l.out.String() + l.held
		if EstimateTokens(total) > l.maxTokens {
			cut := cutTokens(total, l.maxTokens)
			emit = strings.TrimPrefix(cut, l.out.String())
			l.out.WriteString(emit)
			l.held = ""
			l.done = true
			l.finish = "length"
			return emit, true
		}
	}
	if emit, ok := l.matchStop(); ok {
		return emit, true
	}
	if l.hold == 0 {
		emit = l.held
		l.out.WriteString(emit)
		l.held = ""
		return emit, false
	}
	runes := []rune(l.held)
	if len(runes) <= l.hold {
		return "", false
	}
	emit = string(runes[:len(runes)-l.hold])
	l.held = string(runes[len(runes)-l.hold:])
	l.out.WriteString(emit)
	return emit, false
}

func (l *limiter) matchStop() (string, bool) {
	total := l.out.String() + l.held
	best := -1
	var matched string
	for _, st := range l.stop {
		i := strings.Index(total, st)
		if i < 0 || i+len(st) <= l.out.Len() {
			continue
		}
		if best < 0 || i < best {
			best = i
			matched = st
		}
	}
	if best < 0 {
		return "", false
	}
	visible := total[:best]
	emit := strings.TrimPrefix(visible, l.out.String())
	l.out.Reset()
	l.out.WriteString(visible)
	l.held = ""
	l.done = true
	l.finish = "stop_sequence"
	l.matched = matched
	return emit, true
}

// Flush emits text held back to detect a stop sequence.
func (l *limiter) Flush() string {
	if l == nil || l.done {
		return ""
	}
	emit := l.held
	l.out.WriteString(emit)
	l.held = ""
	return emit
}

func (l *limiter) Text() string {
	if l == nil {
		return ""
	}
	return l.out.String()
}

func applyLimits(text string, stop []string, maxTokens int) (out, finish, matched string) {
	if len(stop) == 0 && maxTokens <= 0 {
		return text, "", ""
	}
	l := newLimiter(stop, maxTokens)
	l.Push(text)
	l.Flush()
	return l.Text(), l.finish, l.matched
}

func cutTokens(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	lo, hi := 0, len(r)
	best := 0
	for lo <= hi {
		mid := (lo + hi) / 2
		if EstimateTokens(string(r[:mid])) <= max {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return string(r[:best])
}
